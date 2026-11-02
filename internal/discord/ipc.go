package discord

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	ipcHandshakeOpcode = 0
	ipcFrameOpcode     = 1
	ipcMaxFrame        = 1 << 20
	ipcDisableAfter    = 10 * time.Minute
)

type ipcDependencies struct {
	dial         func(context.Context) (net.Conn, error)
	now          func() time.Time
	retry        func(int) time.Duration
	disableAfter time.Duration
	timeout      time.Duration
}

func defaultIPCDependencies() ipcDependencies {
	return ipcDependencies{
		dial: dialDiscordIPC,
		now:  time.Now,
		retry: func(attempt int) time.Duration {
			return min(30*time.Second, time.Second<<min(attempt, 5))
		},
		disableAfter: ipcDisableAfter,
		timeout:      5 * time.Second,
	}
}

type ipcClient struct {
	mu        sync.Mutex
	settings  Settings
	configRev uint64
	nextID    SessionID
	activeID  SessionID
	desired   Snapshot
	status    Status
	closed    bool
	wake      chan struct{}
	done      chan struct{}
	deps      ipcDependencies
}

func newIPCClient(ctx context.Context, deps ipcDependencies) *ipcClient {
	c := &ipcClient{wake: make(chan struct{}, 1), done: make(chan struct{}), deps: deps, status: Status{State: Disabled, Transport: TransportIPC}}
	go c.run(ctx)
	return c
}

func (c *ipcClient) Configure(settings Settings) {
	settings = settings.withDefaults()
	c.mu.Lock()
	if !c.closed {
		c.settings = settings
		c.configRev++
		c.status = Status{
			State: Disabled, Enabled: settings.Enabled, HasToken: settings.Token != "",
			HasApplicationID: settings.ApplicationID != "", ShowBrowsing: settings.ShowBrowsing,
			ShowArtwork: settings.ShowArtwork, Transport: TransportIPC,
		}
	}
	c.mu.Unlock()
	c.notify()
}

func (c *ipcClient) Begin(snapshot Snapshot) SessionID {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0
	}
	c.nextID++
	c.activeID, c.desired = c.nextID, snapshot
	c.notify()
	return c.activeID
}

func (c *ipcClient) Update(id SessionID, snapshot Snapshot) {
	c.mu.Lock()
	if !c.closed && id != 0 && id == c.activeID && (snapshot.ObservedAt.IsZero() || !snapshot.ObservedAt.Before(c.desired.ObservedAt)) {
		c.desired = snapshot
	}
	c.mu.Unlock()
	c.notify()
}

func (c *ipcClient) End(id SessionID) {
	c.mu.Lock()
	if id != 0 && id == c.activeID {
		c.activeID, c.desired = 0, Snapshot{}
	}
	c.mu.Unlock()
	c.notify()
}

func (c *ipcClient) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *ipcClient) Close(ctx context.Context) error {
	c.mu.Lock()
	c.closed = true
	c.activeID, c.desired = 0, Snapshot{}
	c.mu.Unlock()
	c.notify()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *ipcClient) notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *ipcClient) current() (Settings, Snapshot, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings, c.desired, c.configRev, c.closed
}

func (c *ipcClient) setStatus(update func(*Status)) {
	c.mu.Lock()
	update(&c.status)
	c.mu.Unlock()
}

func (c *ipcClient) run(ctx context.Context) {
	defer close(c.done)
	var conn net.Conn
	var revision uint64
	var settings Settings
	var retryAt, failureStarted time.Time
	var attempt int
	var lastPayload []byte
	var nonce uint64
	closeConnection := func(clear bool) {
		if conn == nil {
			return
		}
		if clear {
			nonce++
			clearCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = ipcSetActivity(clearCtx, conn, settings, nil, nonce)
			cancel()
		}
		_ = conn.Close()
		conn = nil
	}
	defer closeConnection(true)

	for {
		configured, snapshot, currentRevision, closed := c.current()
		if closed || ctx.Err() != nil {
			return
		}
		now := c.deps.now()
		if currentRevision != revision {
			closeConnection(true)
			settings, revision = configured, currentRevision
			retryAt, failureStarted = time.Time{}, time.Time{}
			attempt, lastPayload = 0, nil
			c.setStatus(func(status *Status) {
				status.AutoDisabled = false
				status.Enabled = settings.Enabled
				if settings.Enabled {
					status.State, status.Message = Connecting, "Waiting for the Discord desktop client"
				} else {
					status.State, status.Message = Disabled, "Sharing is off"
				}
			})
		}

		if settings.Enabled && conn == nil && !retryAt.After(now) {
			dialCtx, cancel := context.WithTimeout(ctx, c.deps.timeout)
			candidate, err := c.deps.dial(dialCtx)
			if err == nil {
				err = ipcHandshake(dialCtx, candidate, settings.ApplicationID)
			}
			cancel()
			if err == nil {
				conn = candidate
				attempt, retryAt, failureStarted = 0, time.Time{}, time.Time{}
				lastPayload = nil
				c.setStatus(func(status *Status) {
					status.State, status.Message, status.AutoDisabled = Ready, "Connected through Discord desktop", false
				})
			} else {
				if candidate != nil {
					_ = candidate.Close()
				}
				if failureStarted.IsZero() {
					failureStarted = now
				}
				if now.Sub(failureStarted) >= c.deps.disableAfter {
					c.setStatus(func(status *Status) {
						status.State, status.Message = Disabled, "Discord Rich Presence disabled after repeated desktop connection failures"
						status.Enabled, status.AutoDisabled = false, true
					})
					settings.Enabled = false
				} else {
					retryAt = now.Add(c.deps.retry(attempt))
					attempt++
				}
			}
		}

		if conn != nil {
			value := formatActivity(settings, snapshot, settings.LargeImage, now)
			payload := ipcActivityPayload(value)
			if !bytes.Equal(payload, lastPayload) {
				nonce++
				writeCtx, cancel := context.WithTimeout(ctx, c.deps.timeout)
				err := ipcSetActivity(writeCtx, conn, settings, value, nonce)
				cancel()
				if err != nil {
					closeConnection(false)
					if failureStarted.IsZero() {
						failureStarted = now
					}
					retryAt = now.Add(c.deps.retry(attempt))
					attempt++
					c.setStatus(func(status *Status) {
						status.State, status.Message = Connecting, "Waiting for the Discord desktop client"
					})
				} else {
					lastPayload = payload
					c.setStatus(func(status *Status) { status.LastSent = now })
				}
			}
		}

		delay := time.Second
		if conn == nil && settings.Enabled && retryAt.After(now) {
			delay = min(delay, retryAt.Sub(now))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func ipcHandshake(ctx context.Context, conn net.Conn, applicationID string) error {
	payload, _ := json.Marshal(map[string]any{"v": 1, "client_id": applicationID})
	if err := writeIPCFrame(ctx, conn, ipcHandshakeOpcode, payload); err != nil {
		return err
	}
	_, response, err := readIPCFrame(ctx, conn)
	if err != nil {
		return err
	}
	var ready struct {
		Event string `json:"evt"`
	}
	if json.Unmarshal(response, &ready) != nil || ready.Event != "READY" {
		return fmt.Errorf("Discord IPC handshake did not become ready")
	}
	return nil
}

func ipcSetActivity(ctx context.Context, conn net.Conn, settings Settings, value *activity, nonce uint64) error {
	var activityValue any
	if value != nil {
		activityValue = ipcActivityFrom(value)
	}
	payload, _ := json.Marshal(map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": os.Getpid(), "activity": activityValue},
		"nonce": strconv.FormatUint(nonce, 10),
	})
	if err := writeIPCFrame(ctx, conn, ipcFrameOpcode, payload); err != nil {
		return err
	}
	_, _, err := readIPCFrame(ctx, conn)
	return err
}

func ipcActivityPayload(value *activity) []byte {
	if value == nil {
		return []byte("null")
	}
	data, _ := json.Marshal(ipcActivityFrom(value))
	return data
}

func ipcActivityFrom(value *activity) map[string]any {
	result := map[string]any{"details": value.Details, "state": value.State}
	if value.Type != 0 {
		result["type"] = value.Type
	}
	if value.Timestamps != nil {
		timestamps := map[string]int64{}
		if value.Timestamps.Start > 0 {
			timestamps["start"] = value.Timestamps.Start / 1000
		}
		if value.Timestamps.End > 0 {
			timestamps["end"] = value.Timestamps.End / 1000
		}
		result["timestamps"] = timestamps
	}
	if value.Assets != nil {
		result["assets"] = value.Assets
	}
	if value.Metadata != nil && len(value.Metadata.URLs) > 0 && len(value.Buttons) > 0 {
		result["buttons"] = []map[string]string{{"label": value.Buttons[0], "url": value.Metadata.URLs[0]}}
	}
	return result
}

func writeIPCFrame(ctx context.Context, conn net.Conn, opcode uint32, payload []byte) error {
	if len(payload) > ipcMaxFrame {
		return fmt.Errorf("Discord IPC payload is too large")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[:4], opcode)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(payload)))
	frame := append(header, payload...)
	for len(frame) > 0 {
		written, err := conn.Write(frame)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		frame = frame[written:]
	}
	return nil
}

func readIPCFrame(ctx context.Context, conn net.Conn) (uint32, []byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	size := binary.LittleEndian.Uint32(header[4:])
	if size > ipcMaxFrame {
		return 0, nil, fmt.Errorf("Discord IPC frame is too large")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return binary.LittleEndian.Uint32(header[:4]), payload, nil
}
