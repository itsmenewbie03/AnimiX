package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRealWebSocketTransportAcceptsLargeReadyAndClearsOnClose(t *testing.T) {
	received := make(chan packet, 8)
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		write := func(value any) {
			data, _ := json.Marshal(value)
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				serverErrors <- err
			}
		}
		write(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 60000}})
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					serverErrors <- err
				}
				return
			}
			var p packet
			if err := json.Unmarshal(data, &p); err != nil {
				serverErrors <- err
				return
			}
			received <- p
			if p.Op == 2 {
				write(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]any{
					"session_id": "socket-fixture", "extra": strings.Repeat("x", 80<<10),
				}})
			}
		}
	}))
	defer server.Close()
	deps := defaultDependencies()
	deps.url = "ws" + strings.TrimPrefix(server.URL, "http")
	deps.jitter = func(interval time.Duration) time.Duration { return interval / 2 }
	client := newClient(context.Background(), deps)
	defer client.Close(context.Background())
	client.Begin(watching("Live local transport"))
	client.Configure(Settings{Enabled: true, Token: "synthetic-fixture"})
	for {
		select {
		case p := <-received:
			if p.Op != 3 {
				continue
			}
			if got := activities(t, p); len(got) != 1 || got[0].Details != "Live local transport" {
				t.Fatalf("presence = %#v", got)
			}
			if err := client.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case cleared := <-received:
				if cleared.Op != 3 || len(activities(t, cleared)) != 0 {
					t.Fatal("real connection was not cleared before close")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no clear before close")
			}
			return
		case err := <-serverErrors:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatal("real transport did not publish")
		}
	}
}

func TestOldSocketCallbacksCannotChangeReplacementConnection(t *testing.T) {
	settings := Settings{Enabled: true, Token: "fixture"}
	client := &Client{settings: settings, configRev: 1, status: Status{State: Ready}}
	current, lateDial := newConnection(), newConnection()
	seq, staleSeq := int64(42), int64(999)
	g := &gateway{client: client, settings: settings, configRev: 1, epoch: 2, conn: current, ready: true, sequence: &seq}
	g.receive(gatewayEvent{epoch: 1, err: io.EOF})
	g.receive(gatewayEvent{epoch: 1, data: packet{Op: 0, T: "RESUMED", S: &staleSeq}})
	g.receive(gatewayEvent{epoch: 1, conn: lateDial})
	if !g.ready || g.conn != current || *g.sequence != 42 || client.Status().State != Ready {
		t.Fatal("old socket callback changed the current connection")
	}
	select {
	case <-lateDial.closed:
	default:
		t.Fatal("unadopted late socket was not closed")
	}
	select {
	case <-current.closed:
		t.Fatal("old callback closed the replacement socket")
	default:
	}
}

func TestQueuedHelloCannotAuthenticateAfterConfigurationChanges(t *testing.T) {
	for _, next := range []Settings{{}, {Enabled: true, Token: "replacement"}} {
		settings := Settings{Enabled: true, Token: "old"}
		client := &Client{settings: settings, configRev: 1, wake: make(chan struct{}, 1)}
		conn := newConnection()
		g := &gateway{client: client, settings: settings, configRev: 1, epoch: 2, conn: conn, deps: defaultDependencies()}
		client.Configure(next)
		g.receive(gatewayEvent{epoch: 2, data: packet{Op: 10, D: json.RawMessage(`{"heartbeat_interval":60000}`)}})
		if len(conn.out) != 0 || g.hello {
			t.Fatal("queued Hello authenticated using obsolete settings")
		}
		if g.write(presencePayload(formatActivity(settings, watching("Old activity"), "", time.Now()))) {
			t.Fatal("obsolete connection accepted an activity write")
		}
	}
}

func TestCloseCodeRecoveryPolicy(t *testing.T) {
	for _, test := range []struct {
		code        int
		state       State
		resumable   bool
		minimumWait time.Duration
	}{
		{4004, NeedsLogin, true, 0},
		{4007, RetryWait, false, 0},
		{4009, RetryWait, false, 0},
		{4008, RetryWait, true, 20 * time.Second},
		{4013, Failed, true, 0},
		{1000, RetryWait, false, 0},
		{1006, RetryWait, true, 0},
	} {
		now := time.Now()
		client := &Client{}
		deps := defaultDependencies()
		deps.now = func() time.Time { return now }
		g := &gateway{client: client, deps: deps, session: "existing"}
		g.connectionError(websocket.CloseError{Code: websocket.StatusCode(test.code)})
		if client.Status().State != test.state || (g.session != "") != test.resumable || g.retryAt.Before(now.Add(test.minimumWait)) && test.minimumWait > 0 {
			t.Errorf("code %d: status=%#v, resumable=%v, retry=%v", test.code, client.Status(), g.session != "", g.retryAt)
		}
	}
}
