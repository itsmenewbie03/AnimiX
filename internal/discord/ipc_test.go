package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPCHandshakePublishesAndClearsActivity(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	requests := make(chan map[string]any, 8)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer serverSide.Close()
		_, handshake, err := readIPCFrame(context.Background(), serverSide)
		if err != nil {
			return
		}
		var hello map[string]any
		_ = json.Unmarshal(handshake, &hello)
		requests <- hello
		ready, _ := json.Marshal(map[string]any{"cmd": "DISPATCH", "evt": "READY"})
		_ = writeIPCFrame(context.Background(), serverSide, ipcFrameOpcode, ready)
		for {
			_, payload, err := readIPCFrame(context.Background(), serverSide)
			if err != nil {
				return
			}
			var request map[string]any
			_ = json.Unmarshal(payload, &request)
			requests <- request
			response, _ := json.Marshal(map[string]any{"cmd": "SET_ACTIVITY", "nonce": request["nonce"]})
			if writeIPCFrame(context.Background(), serverSide, ipcFrameOpcode, response) != nil {
				return
			}
		}
	}()

	deps := defaultIPCDependencies()
	var dialed atomic.Bool
	deps.dial = func(context.Context) (net.Conn, error) {
		if dialed.Swap(true) {
			return nil, errors.New("unexpected reconnect")
		}
		return clientSide, nil
	}
	deps.retry = func(int) time.Duration { return time.Hour }
	client := newIPCClient(context.Background(), deps)
	snapshot := watching("IPC fixture")
	snapshot.CoverURL = "https://images.example/cover.jpg"
	client.Begin(snapshot)
	client.Configure(Settings{Enabled: true, Transport: TransportIPC, ShowArtwork: true, LargeImage: "fallback-asset"})
	awaitIPCStatus(t, clientStatus{client}, func(status Status) bool { return status.State == Ready && !status.LastSent.IsZero() })

	hello := <-requests
	if hello["client_id"] != DefaultApplicationID {
		t.Fatalf("handshake = %#v", hello)
	}
	published := <-requests
	args := published["args"].(map[string]any)
	activity := args["activity"].(map[string]any)
	if published["cmd"] != "SET_ACTIVITY" || activity["details"] != "IPC fixture" {
		t.Fatalf("activity request = %#v", published)
	}
	assets, ok := activity["assets"].(map[string]any)
	if !ok || assets["large_image"] != snapshot.CoverURL {
		t.Fatalf("IPC artwork = %#v", activity["assets"])
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	cleared := <-requests
	if args := cleared["args"].(map[string]any); args["activity"] != nil {
		t.Fatalf("close did not clear activity: %#v", cleared)
	}
	<-serverDone
}

func TestIPCImagePrefersPublicSourceCover(t *testing.T) {
	t.Parallel()
	cover := "https://images.example/cover.jpg"
	tests := []struct {
		name     string
		settings Settings
		cover    string
		want     string
	}{
		{name: "enabled uses source cover", settings: Settings{ShowArtwork: true, LargeImage: "asset"}, cover: cover, want: cover},
		{name: "disabled keeps large image", settings: Settings{LargeImage: "asset"}, cover: cover, want: "asset"},
		{name: "invalid cover keeps large image", settings: Settings{ShowArtwork: true, LargeImage: "asset"}, cover: "http://images.example/cover.jpg", want: "asset"},
		{name: "local cover is rejected", settings: Settings{ShowArtwork: true, LargeImage: "asset"}, cover: "https://localhost/cover.jpg", want: "asset"},
		{name: "no images", settings: Settings{ShowArtwork: true}, cover: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := Snapshot{CoverURL: test.cover}
			if got := ipcImage(test.settings, snapshot); got != test.want {
				t.Fatalf("image = %q, want %q", got, test.want)
			}
		})
	}
}

type clientStatus struct{ client *ipcClient }

func (c clientStatus) Status() Status { return c.client.Status() }

type statusReader interface{ Status() Status }

func awaitIPCStatus(t *testing.T, reader statusReader, matches func(Status) bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !matches(reader.Status()) {
		select {
		case <-deadline:
			t.Fatalf("IPC status not reached: %#v", reader.Status())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestIPCDisablesAfterTenMinutesOfFailures(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	deps := defaultIPCDependencies()
	deps.now = func() time.Time { return time.Unix(0, now.Load()) }
	var attempts atomic.Int32
	deps.dial = func(context.Context) (net.Conn, error) {
		attempts.Add(1)
		return nil, errors.New("Discord is not running")
	}
	deps.retry = func(int) time.Duration { return 0 }
	deps.disableAfter = 10 * time.Minute
	client := newIPCClient(context.Background(), deps)
	defer client.Close(context.Background())
	client.Configure(Settings{Enabled: true, Transport: TransportIPC})
	awaitIPCStatus(t, clientStatus{client}, func(status Status) bool { return attempts.Load() > 0 && status.State == Connecting })
	now.Add(int64(10*time.Minute + time.Second))
	client.notify()
	awaitIPCStatus(t, clientStatus{client}, func(status Status) bool { return status.AutoDisabled })
	status := client.Status()
	if status.Enabled || status.Transport != TransportIPC {
		t.Fatalf("auto-disabled status = %#v", status)
	}
}
