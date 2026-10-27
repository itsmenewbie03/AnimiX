package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const maxMessageSize = 32 * 1024 * 1024

type Request struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
	ID     string         `json:"id,omitempty"`
}

type Response struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Event     string          `json:"event"`
	RequestID string          `json:"requestId"`
	Data      json.RawMessage `json:"data"`
	Error     json.RawMessage `json:"error"`
}

type CloudflareChallenge struct {
	URL        string `json:"url"`
	StatusCode int    `json:"statusCode"`
	UserAgent  string `json:"userAgent"`
}

type CloudflareError struct {
	Challenge CloudflareChallenge
}

func (e *CloudflareError) Error() string {
	return fmt.Sprintf("cloudflare challenge (HTTP %d) at %s", e.Challenge.StatusCode, e.Challenge.URL)
}

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writes  sync.Mutex
	pending sync.Map
	nextID  atomic.Uint64
	done    chan struct{}
	logs    chan string
	once    sync.Once
}

type pendingCall struct {
	responses chan Response
}

func Start(ctx context.Context, javaPath, jarPath string) (*Client, error) {
	cmd := exec.CommandContext(ctx, javaPath,
		"-Dfile.encoding=UTF-8",
		"-Dsun.stdout.encoding=UTF-8",
		"-Dsun.stderr.encoding=UTF-8",
		"-Xms256m",
		"-Xmx2048m",
		"-noverify",
		"-jar", jarPath,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stderr: %w", err)
	}

	c := &Client{
		cmd:   cmd,
		stdin: stdin,
		done:  make(chan struct{}),
		logs:  make(chan string, 128),
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}

	ready := make(chan struct{})
	go c.scanResponses(stdout)
	go c.scanLogs(stderr, ready)
	go func() {
		err := cmd.Wait()
		c.shutdown(err)
	}()

	select {
	case <-ready:
		return c, nil
	case <-time.After(10 * time.Second):
		// The upstream Dart client also continues if the readiness line is absent.
		return c, nil
	case <-c.done:
		return nil, errors.New("bridge exited during startup")
	case <-ctx.Done():
		_ = c.Close()
		return nil, ctx.Err()
	}
}

func (c *Client) Logs() <-chan string { return c.logs }

func (c *Client) Call(ctx context.Context, method string, args map[string]any, out any) error {
	id := fmt.Sprintf("req_%d", c.nextID.Add(1))
	call := &pendingCall{responses: make(chan Response, 8)}
	c.pending.Store(id, call)
	defer c.pending.Delete(id)

	if err := c.write(Request{Method: method, Args: args, ID: id}); err != nil {
		return err
	}

	for {
		select {
		case response, ok := <-call.responses:
			if !ok {
				return errors.New("bridge closed before responding")
			}
			if response.Event == "cloudflare" {
				_ = c.write(Request{Method: "cancel", Args: map[string]any{"id": id}})
				var challenge CloudflareChallenge
				if err := json.Unmarshal(response.Data, &challenge); err != nil {
					return fmt.Errorf("decode Cloudflare event: %w", err)
				}
				return &CloudflareError{Challenge: challenge}
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return fmt.Errorf("bridge %s: %s", method, readableJSON(response.Error))
			}
			if response.Status == "partial" {
				continue
			}
			if out == nil || len(response.Data) == 0 || string(response.Data) == "null" {
				return nil
			}
			if err := json.Unmarshal(response.Data, out); err != nil {
				return fmt.Errorf("decode %s response: %w", method, err)
			}
			return nil
		case <-ctx.Done():
			_ = c.write(Request{Method: "cancel", Args: map[string]any{"id": id}})
			return ctx.Err()
		case <-c.done:
			return errors.New("bridge process stopped")
		}
	}
}

func (c *Client) write(request Request) error {
	c.writes.Lock()
	defer c.writes.Unlock()
	select {
	case <-c.done:
		return errors.New("bridge is closed")
	default:
	}
	return json.NewEncoder(c.stdin).Encode(request)
}

func (c *Client) scanResponses(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxMessageSize)
	for scanner.Scan() {
		var response Response
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			c.publishLog("invalid bridge response: " + scanner.Text())
			continue
		}
		responseID := response.ID
		if response.Event != "" {
			responseID = response.RequestID
		}
		if response.Event == "cloudflare" && responseID == "" {
			// Some extension calls move onto an untagged worker thread. The event
			// still has to interrupt the host instead of being silently discarded.
			c.pending.Range(func(_, value any) bool {
				select {
				case value.(*pendingCall).responses <- response:
				case <-c.done:
					return false
				}
				return true
			})
			continue
		}
		value, ok := c.pending.Load(responseID)
		if !ok {
			continue
		}
		select {
		case value.(*pendingCall).responses <- response:
		case <-c.done:
		}
	}
	if err := scanner.Err(); err != nil {
		c.publishLog("bridge stdout: " + err.Error())
	}
}

func (c *Client) scanLogs(reader io.Reader, ready chan<- struct{}) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxMessageSize)
	readyOnce := sync.Once{}
	for scanner.Scan() {
		line := scanner.Text()
		c.publishLog(line)
		if containsStartupSignal(line) {
			readyOnce.Do(func() { close(ready) })
		}
	}
}

func containsStartupSignal(line string) bool {
	return len(line) >= len("AnymeX Sidecar Process Started") &&
		findSubstring(line, "AnymeX Sidecar Process Started")
}

func findSubstring(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}

func readableJSON(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func (c *Client) publishLog(line string) {
	select {
	case c.logs <- line:
	default:
	}
}

func (c *Client) shutdown(err error) {
	c.once.Do(func() {
		if err != nil {
			c.publishLog("bridge stopped: " + err.Error())
		}
		close(c.done)
	})
}

func (c *Client) Close() error {
	var err error
	if c.cmd.Process != nil {
		err = c.cmd.Process.Kill()
	}
	return err
}
