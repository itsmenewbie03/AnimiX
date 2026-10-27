package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type recordingWriteCloser struct {
	writes chan []byte
}

func (w *recordingWriteCloser) Write(data []byte) (int, error) {
	copyOfData := append([]byte(nil), data...)
	w.writes <- copyOfData
	return len(data), nil
}

func (*recordingWriteCloser) Close() error { return nil }

func TestCloudflareEventInterruptsCorrelatedCall(t *testing.T) {
	t.Parallel()

	writer := &recordingWriteCloser{writes: make(chan []byte, 2)}
	client := &Client{
		stdin: writer,
		done:  make(chan struct{}),
		logs:  make(chan string, 1),
	}

	errResult := make(chan error, 1)
	go func() {
		errResult <- client.Call(context.Background(), "search", nil, nil)
	}()

	var request Request
	if err := json.Unmarshal(<-writer.writes, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if request.ID != "req_1" {
		t.Fatalf("request id = %q, want req_1", request.ID)
	}

	event := `{"event":"cloudflare","requestId":"req_1","data":{"url":"https://example.com/search","statusCode":403,"userAgent":"test-agent"}}`
	client.scanResponses(strings.NewReader(event + "\n"))

	var challengeError *CloudflareError
	if err := <-errResult; !errors.As(err, &challengeError) {
		t.Fatalf("Call error = %v, want CloudflareError", err)
	}
	if challengeError.Challenge.URL != "https://example.com/search" || challengeError.Challenge.StatusCode != 403 {
		t.Fatalf("challenge = %#v", challengeError.Challenge)
	}

	var cancel Request
	if err := json.Unmarshal(<-writer.writes, &cancel); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if cancel.Method != "cancel" || cancel.Args["id"] != "req_1" {
		t.Fatalf("cancel request = %#v", cancel)
	}
}

func TestUncorrelatedCloudflareEventReachesPendingCalls(t *testing.T) {
	t.Parallel()

	client := &Client{
		done: make(chan struct{}),
		logs: make(chan string, 1),
	}
	pending := &pendingCall{responses: make(chan Response, 1)}
	client.pending.Store("req_1", pending)

	event := `{"event":"cloudflare","data":{"url":"https://example.com","statusCode":503}}`
	client.scanResponses(strings.NewReader(event + "\n"))

	select {
	case response := <-pending.responses:
		if response.Event != "cloudflare" {
			t.Fatalf("event = %q, want cloudflare", response.Event)
		}
	default:
		t.Fatal("uncorrelated Cloudflare event was discarded")
	}
}

var _ io.WriteCloser = (*recordingWriteCloser)(nil)
