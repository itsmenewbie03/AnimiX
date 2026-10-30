package discord

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const gatewayURL = "wss://gateway.discord.gg/?v=9&encoding=json"

type connection interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close(normal bool)
}

type dependencies struct {
	dial       func(context.Context, string) (connection, error)
	now        func() time.Time
	jitter     func(time.Duration) time.Duration
	retry      func(int) time.Duration
	asset      func(context.Context, Settings, string) (string, error)
	url        string
	wakeEvery  time.Duration
	handshake  time.Duration
	writeLimit time.Duration
}

func defaultDependencies() dependencies {
	return dependencies{
		dial: func(ctx context.Context, url string) (connection, error) {
			conn, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
			if err != nil {
				if response != nil && response.Body != nil {
					response.Body.Close()
				}
				return nil, err
			}
			conn.SetReadLimit(16 << 20) // READY can be much larger than an outgoing event.
			return &gatewayConnection{conn}, nil
		},
		now: time.Now,
		jitter: func(interval time.Duration) time.Duration {
			return time.Duration(rand.Float64() * float64(interval))
		},
		retry: func(attempt int) time.Duration {
			base := min(30*time.Second, time.Second<<min(attempt, 5))
			return base/2 + time.Duration(rand.Float64()*float64(base/2))
		},
		asset:      assetResolver(&http.Client{Timeout: 5 * time.Second}),
		url:        gatewayURL,
		wakeEvery:  time.Second,
		handshake:  20 * time.Second,
		writeLimit: 2 * time.Second,
	}
}

type gatewayConnection struct{ *websocket.Conn }

func (c *gatewayConnection) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.Conn.Read(ctx)
	return data, err
}

func (c *gatewayConnection) Write(ctx context.Context, data []byte) error {
	return c.Conn.Write(ctx, websocket.MessageText, data)
}

func (c *gatewayConnection) Close(normal bool) {
	if !normal {
		_ = c.CloseNow()
		return
	}
	// A normal close invalidates the Discord session. Bound the peer's close
	// handshake independently of the already-canceled application context.
	done := make(chan struct{})
	go func() {
		_ = c.Conn.Close(websocket.StatusNormalClosure, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		_ = c.CloseNow()
		<-done
	}
}
