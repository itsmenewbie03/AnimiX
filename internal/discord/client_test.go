package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type fakeConnection struct {
	in     chan []byte
	out    chan []byte
	errors chan error
	closed chan struct{}
	once   sync.Once
}

func newConnection() *fakeConnection {
	return &fakeConnection{in: make(chan []byte, 32), out: make(chan []byte, 128), errors: make(chan error, 1), closed: make(chan struct{})}
}

func (f *fakeConnection) Read(ctx context.Context) ([]byte, error) {
	select {
	case data := <-f.in:
		return data, nil
	case err := <-f.errors:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.closed:
		return nil, io.EOF
	}
}

func (f *fakeConnection) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.closed:
		return io.ErrClosedPipe
	case f.out <- append([]byte(nil), data...):
		return nil
	}
}

func (f *fakeConnection) Close(bool) { f.once.Do(func() { close(f.closed) }) }

func (f *fakeConnection) deliver(t *testing.T, op int, d any, event string, sequence *int64) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"op": op, "d": d, "t": event, "s": sequence})
	if err != nil {
		t.Fatal(err)
	}
	f.in <- data
}

func (f *fakeConnection) next(t *testing.T, op int) packet {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-f.out:
			var p packet
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			if p.Op == op {
				return p
			}
		case <-deadline:
			t.Fatalf("no outgoing opcode %d", op)
		}
	}
}

type harness struct {
	client *Client
	dials  chan *fakeConnection
	now    atomic.Int64
	count  atomic.Int32
}

func newHarness(t *testing.T, change func(*dependencies)) *harness {
	t.Helper()
	h := &harness{dials: make(chan *fakeConnection, 16)}
	h.now.Store(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC).UnixNano())
	deps := defaultDependencies()
	deps.now = func() time.Time { return time.Unix(0, h.now.Load()) }
	deps.wakeEvery = time.Hour
	deps.jitter = func(interval time.Duration) time.Duration { return interval / 2 }
	deps.retry = func(int) time.Duration { return 0 }
	deps.dial = func(context.Context, string) (connection, error) {
		h.count.Add(1)
		conn := newConnection()
		h.dials <- conn
		return conn, nil
	}
	if change != nil {
		change(&deps)
	}
	h.client = newClient(context.Background(), deps)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := h.client.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return h
}

func (h *harness) advance(d time.Duration) {
	h.now.Add(int64(d))
	h.client.notify()
}

func (h *harness) connect(t *testing.T) *fakeConnection {
	t.Helper()
	select {
	case conn := <-h.dials:
		return conn
	case <-time.After(2 * time.Second):
		t.Fatal("Gateway did not connect")
		return nil
	}
}

func authenticate(t *testing.T, conn *fakeConnection, resume bool) {
	t.Helper()
	conn.deliver(t, 10, map[string]any{"heartbeat_interval": 60000}, "", nil)
	op := 2
	if resume {
		op = 6
	}
	conn.next(t, op)
	seq := int64(42)
	if resume {
		conn.deliver(t, 0, map[string]any{}, "RESUMED", &seq)
	} else {
		conn.deliver(t, 0, map[string]any{
			"session_id": "fixture", "resume_gateway_url": "wss://gateway-test.discord.gg",
			"user": map[string]string{"username": "fixture-user"},
		}, "READY", &seq)
	}
}

func activities(t *testing.T, p packet) []activity {
	t.Helper()
	var result struct {
		Activities []activity `json:"activities"`
	}
	if err := json.Unmarshal(p.D, &result); err != nil {
		t.Fatal(err)
	}
	return result.Activities
}

func watching(title string) Snapshot {
	return Snapshot{Kind: Watching, Title: title, Number: "2", Active: true}
}

func awaitState(t *testing.T, c *Client, state State) {
	t.Helper()
	awaitStatus(t, c, func(status Status) bool { return status.State == state })
}

func awaitStatus(t *testing.T, c *Client, matches func(Status) bool) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(2 * time.Second)
	for !matches(c.Status()) {
		select {
		case <-ticker.C:
		case <-timeout:
			t.Fatalf("expected connection status was not reached: %#v", c.Status())
		}
	}
}

func TestDisabledNeverDialsEvenWithSavedTokenAndActivity(t *testing.T) {
	var artworkCalls atomic.Int32
	h := newHarness(t, func(deps *dependencies) {
		deps.asset = func(context.Context, Settings, string) (string, error) {
			artworkCalls.Add(1)
			return "", nil
		}
	})
	snapshot := watching("Private activity")
	snapshot.CoverURL = "https://images.example/cover.jpg"
	h.client.Begin(snapshot)
	h.client.Configure(Settings{Token: "fixture", ApplicationID: "123", ShowArtwork: true})
	awaitStatus(t, h.client, func(status Status) bool { return status.Message == "Sharing is off" })
	if err := h.client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.count.Load() != 0 || artworkCalls.Load() != 0 {
		t.Fatal("disabled presence made a Gateway or artwork request")
	}
}

func TestEnabledWithoutTokenWaitsForLogin(t *testing.T) {
	h := newHarness(t, nil)
	h.client.Configure(Settings{Enabled: true})
	awaitState(t, h.client, NeedsLogin)
	if h.count.Load() != 0 {
		t.Fatal("client connected without a credential")
	}
}

func TestActivityBeforeReadyAndOfflineUpdatesAreReplayed(t *testing.T) {
	h := newHarness(t, nil)
	id := h.client.Begin(watching("Before Ready"))
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	first := h.connect(t)
	authenticate(t, first, false)
	if got := activities(t, first.next(t, 3)); len(got) != 1 || got[0].Details != "Before Ready" {
		t.Fatalf("first presence = %#v", got)
	}
	first.errors <- io.EOF
	second := h.connect(t)
	h.client.Update(id, watching("Changed while offline"))
	h.advance(5 * time.Second)
	authenticate(t, second, true)
	if got := activities(t, second.next(t, 3)); len(got) != 1 || got[0].Details != "Changed while offline" {
		t.Fatalf("resumed presence = %#v", got)
	}
}

func TestHeartbeatRequestAndACKPreserveDispatchSequence(t *testing.T) {
	h := newHarness(t, nil)
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	conn := h.connect(t)
	authenticate(t, conn, false)
	conn.next(t, 3)
	seq := int64(99)
	conn.deliver(t, 0, map[string]any{}, "UNUSED_DISPATCH", &seq)
	conn.deliver(t, 11, nil, "", nil)
	conn.deliver(t, 1, nil, "", nil)
	if got := string(conn.next(t, 1).D); got != "99" {
		t.Fatalf("heartbeat sequence = %s, want 99", got)
	}
	conn.deliver(t, 11, nil, "", nil)
	conn.deliver(t, 1, nil, "", nil)
	if got := string(conn.next(t, 1).D); got != "99" {
		t.Fatalf("sequence after ACK = %s", got)
	}
}

func TestMissedHeartbeatRecoversWithoutPlaybackEvents(t *testing.T) {
	h := newHarness(t, nil)
	h.client.Begin(watching("Still watching"))
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	conn := h.connect(t)
	authenticate(t, conn, false)
	conn.next(t, 3)
	h.advance(30 * time.Second)
	conn.next(t, 1)
	h.advance(61 * time.Second)
	second := h.connect(t)
	authenticate(t, second, true)
	if got := activities(t, second.next(t, 3)); len(got) != 1 || got[0].Details != "Still watching" {
		t.Fatalf("presence after missed ACK = %#v", got)
	}
}

func TestInvalidSessionIdentifiesAgainAndKeepsActivity(t *testing.T) {
	h := newHarness(t, nil)
	h.client.Begin(watching("Retained"))
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	conn := h.connect(t)
	authenticate(t, conn, false)
	conn.next(t, 3)
	conn.deliver(t, 9, false, "", nil)
	awaitState(t, h.client, RetryWait)
	h.advance(6 * time.Second)
	second := h.connect(t)
	authenticate(t, second, false)
	if got := activities(t, second.next(t, 3)); len(got) != 1 || got[0].Details != "Retained" {
		t.Fatalf("new-session presence = %#v", got)
	}
}

func TestServerReconnectAndResumableInvalidSessionReplayLatest(t *testing.T) {
	for _, op := range []int{7, 9} {
		t.Run(map[int]string{7: "reconnect", 9: "invalid-resumable"}[op], func(t *testing.T) {
			h := newHarness(t, nil)
			id := h.client.Begin(watching("Before reconnect"))
			h.client.Configure(Settings{Enabled: true, Token: "fixture"})
			first := h.connect(t)
			authenticate(t, first, false)
			first.next(t, 3)
			first.deliver(t, op, true, "", nil)
			if op == 9 {
				awaitState(t, h.client, RetryWait)
				h.advance(6 * time.Second)
			}
			second := h.connect(t)
			if op == 7 {
				h.advance(6 * time.Second)
			}
			h.client.Update(id, watching("Latest activity"))
			second.deliver(t, 10, map[string]int{"heartbeat_interval": 60000}, "", nil)
			var resume struct {
				Token   string `json:"token"`
				Session string `json:"session_id"`
				Seq     int64  `json:"seq"`
			}
			if err := json.Unmarshal(second.next(t, 6).D, &resume); err != nil {
				t.Fatal(err)
			}
			if resume.Token != "fixture" || resume.Session != "fixture" || resume.Seq != 42 {
				t.Fatal("resume did not retain authentication and the last dispatch sequence")
			}
			seq := int64(43)
			second.deliver(t, 0, map[string]any{}, "RESUMED", &seq)
			if got := activities(t, second.next(t, 3)); len(got) != 1 || got[0].Details != "Latest activity" {
				t.Fatalf("recovered activity = %#v", got)
			}
		})
	}
}

func TestAuthenticationFailureStopsRetriesAndRedactsErrors(t *testing.T) {
	h := newHarness(t, nil)
	h.client.Configure(Settings{Enabled: true, Token: "do-not-display"})
	conn := h.connect(t)
	conn.errors <- websocket.CloseError{Code: 4004, Reason: "do-not-display"}
	awaitState(t, h.client, NeedsLogin)
	h.advance(time.Hour)
	if err := h.client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.count.Load() != 1 || h.client.Status().Message != "Discord rejected the token; replace it to reconnect" {
		t.Fatalf("auth failure retried or exposed server text: %#v", h.client.Status())
	}
}

func TestOldSessionUpdatesAndCleanupCannotReplaceNewSession(t *testing.T) {
	h := newHarness(t, nil)
	old := h.client.Begin(watching("Old"))
	current := h.client.Begin(watching("New"))
	h.client.Update(old, watching("Late old update"))
	h.client.End(old)
	_, snapshot, id, _, _ := h.client.current()
	if id != current || snapshot.Title != "New" {
		t.Fatalf("old producer changed current activity: %#v", snapshot)
	}
	h.client.End(current)
	h.client.Update(current, watching("Resurrected"))
	_, snapshot, id, _, _ = h.client.current()
	if id != 0 || snapshot.Active {
		t.Fatal("ended activity was resurrected")
	}
}

func TestClearDuringArtworkLookupStaysClear(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	h := newHarness(t, func(deps *dependencies) {
		deps.asset = func(_ context.Context, _ Settings, _ string) (string, error) {
			close(started)
			defer close(finished)
			<-release // Simulate completion racing with cancellation.
			return "mp:late-image", nil
		}
	})
	snapshot := watching("Text comes first")
	snapshot.CoverURL = "https://images.example/cover.jpg"
	id := h.client.Begin(snapshot)
	h.client.Configure(Settings{Enabled: true, Token: "fixture", ApplicationID: "123", ShowArtwork: true})
	conn := h.connect(t)
	authenticate(t, conn, false)
	if got := activities(t, conn.next(t, 3)); len(got) != 1 || got[0].Details != snapshot.Title {
		t.Fatal("artwork blocked initial text")
	}
	<-started
	h.client.End(id)
	if got := activities(t, conn.next(t, 3)); len(got) != 0 {
		t.Fatalf("clear = %#v", got)
	}
	close(release)
	<-finished
	h.advance(30 * time.Second)
	if err := h.client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for len(conn.out) > 0 {
		var p packet
		_ = json.Unmarshal(<-conn.out, &p)
		if p.Op == 3 && len(activities(t, p)) != 0 {
			t.Fatal("late artwork restored cleared activity")
		}
	}
}

func TestSuccessfulArtworkLookupPublishesPosterAfterText(t *testing.T) {
	requested := make(chan string, 1)
	h := newHarness(t, func(deps *dependencies) {
		deps.asset = func(_ context.Context, _ Settings, cover string) (string, error) {
			requested <- cover
			return "mp:external/poster-fixture", nil
		}
	})
	snapshot := watching("Poster fixture")
	snapshot.CoverURL = "https://images.example/poster.webp"
	h.client.Begin(snapshot)
	h.client.Configure(Settings{Enabled: true, Token: "fixture", ApplicationID: "123", ShowArtwork: true})
	conn := h.connect(t)
	authenticate(t, conn, false)
	if got := activities(t, conn.next(t, 3)); len(got) != 1 || got[0].Assets != nil {
		t.Fatalf("initial text presence = %#v", got)
	}
	select {
	case got := <-requested:
		if got != snapshot.CoverURL {
			t.Fatalf("requested cover = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poster conversion was not requested")
	}
	h.advance(5 * time.Second)
	got := activities(t, conn.next(t, 3))
	if len(got) != 1 || got[0].Assets == nil || got[0].Assets.LargeImage != "mp:external/poster-fixture" {
		t.Fatalf("poster presence = %#v", got)
	}
	if status := h.client.Status(); status.ArtworkState != ArtworkReady || status.ArtworkHTTPStatus != 0 {
		t.Fatalf("poster diagnostic = %#v", status)
	}
}

func TestArtworkFailureReportsOnlySafeHTTPStatus(t *testing.T) {
	h := newHarness(t, func(deps *dependencies) {
		deps.asset = func(context.Context, Settings, string) (string, error) {
			return "", artworkError{status: 403}
		}
	})
	snapshot := watching("Rejected poster")
	snapshot.CoverURL = "https://images.example/poster.webp"
	h.client.Begin(snapshot)
	h.client.Configure(Settings{Enabled: true, Token: "secret-fixture", ApplicationID: "123", ShowArtwork: true})
	conn := h.connect(t)
	authenticate(t, conn, false)
	conn.next(t, 3)
	awaitStatus(t, h.client, func(status Status) bool { return status.ArtworkState == ArtworkFailed })
	status := h.client.Status()
	if status.ArtworkHTTPStatus != 403 || strings.Contains(status.Message, "secret-fixture") {
		t.Fatalf("unsafe or incomplete artwork diagnostic = %#v", status)
	}
}

func TestDisabledDuringDialClosesLateSocket(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	late := newConnection()
	h := newHarness(t, func(deps *dependencies) {
		deps.dial = func(context.Context, string) (connection, error) {
			close(started)
			<-release
			return late, nil
		}
	})
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	<-started
	h.client.Configure(Settings{Token: "fixture"})
	awaitState(t, h.client, Disabled)
	close(release)
	select {
	case <-late.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled dial leaked a live socket")
	}
	if len(late.out) != 0 {
		t.Fatal("disabled client authenticated on a late socket")
	}
}

func TestCloseIsBoundedAndStopsProducers(t *testing.T) {
	h := newHarness(t, nil)
	id := h.client.Begin(watching("Watching"))
	h.client.Configure(Settings{Enabled: true, Token: "fixture"})
	conn := h.connect(t)
	authenticate(t, conn, false)
	conn.next(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.client.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := activities(t, conn.next(t, 3)); len(got) != 0 {
		t.Fatal("close did not clear activity")
	}
	h.client.Update(id, watching("After close"))
	if next := h.client.Begin(watching("After close")); next != 0 {
		t.Fatal("closed client accepted a new producer")
	}
	if err := h.client.Close(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
