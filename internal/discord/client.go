package discord

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

type Settings struct {
	Enabled, ShowBrowsing bool
	Token                 string
	// ApplicationID is the user's registered Discord application. Omitting it
	// produces text-only payloads. AnimiX never borrows another app ID.
	ApplicationID string
	LargeImage    string
	ShowArtwork   bool
}

type State string
type ArtworkState string

const (
	Disabled    State = "disabled"
	NeedsLogin  State = "needs_login"
	Connecting  State = "connecting"
	Identifying State = "identifying"
	Resuming    State = "resuming"
	Ready       State = "ready"
	RetryWait   State = "retry_wait"
	Failed      State = "failed"
	Closed      State = "closed"
)

const (
	ArtworkOff     ArtworkState = "off"
	ArtworkWaiting ArtworkState = "waiting_for_activity"
	ArtworkLoading ArtworkState = "loading"
	ArtworkReady   ArtworkState = "ready"
	ArtworkFailed  ArtworkState = "failed"
)

// Status contains only diagnostics safe to display; it never contains a token,
// raw Gateway payload, or server-supplied error text.
type Status struct {
	State                               State
	Enabled, HasToken, HasApplicationID bool
	ShowBrowsing, ShowArtwork           bool
	Username                            string
	Message                             string
	LastACK, LastSent                   time.Time
	RetryAt                             time.Time
	CloseCode                           int
	ArtworkState                        ArtworkState
	ArtworkHTTPStatus                   int
}

type SessionID uint64

// Client's setters only replace in-memory state. A single worker owns all
// Gateway state and writes; its bounded mailbox always represents the newest
// observation, even while offline or while Bubble Tea is suspended.
type Client struct {
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
	deps      dependencies
}

func New(ctx context.Context) *Client { return newClient(ctx, defaultDependencies()) }

func newClient(ctx context.Context, deps dependencies) *Client {
	c := &Client{wake: make(chan struct{}, 1), done: make(chan struct{}), deps: deps, status: Status{State: Disabled}}
	go c.run(ctx)
	return c
}

// Configure is also an explicit retry after an authentication/protocol error.
// A saved token by itself never enables sharing.
func (c *Client) Configure(settings Settings) {
	settings.Token = strings.TrimSpace(settings.Token)
	settings.ApplicationID = strings.TrimSpace(settings.ApplicationID)
	c.mu.Lock()
	if !c.closed {
		c.settings = settings
		c.configRev++
		c.status.Enabled, c.status.HasToken = settings.Enabled, settings.Token != ""
		c.status.HasApplicationID = settings.ApplicationID != ""
		c.status.ShowBrowsing, c.status.ShowArtwork = settings.ShowBrowsing, settings.ShowArtwork
		c.status.ArtworkHTTPStatus = 0
		if settings.ShowArtwork {
			c.status.ArtworkState = ArtworkWaiting
		} else {
			c.status.ArtworkState = ArtworkOff
		}
	}
	c.mu.Unlock()
	c.notify()
}

// Begin replaces the foreground activity. Updates and cleanup from older
// sessions are ignored, including after autoplay or reader navigation.
func (c *Client) Begin(snapshot Snapshot) SessionID {
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

func (c *Client) Update(id SessionID, snapshot Snapshot) {
	c.mu.Lock()
	if !c.closed && id != 0 && c.activeID == id {
		// Producers can finish work out of order. Observation time is assigned at
		// the source, not when the TUI eventually handles a message.
		if snapshot.ObservedAt.IsZero() || !snapshot.ObservedAt.Before(c.desired.ObservedAt) {
			c.desired = snapshot
		}
	}
	c.mu.Unlock()
	c.notify()
}

func (c *Client) End(id SessionID) {
	c.mu.Lock()
	if id != 0 && c.activeID == id {
		c.activeID, c.desired = 0, Snapshot{}
	}
	c.mu.Unlock()
	c.notify()
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Close invalidates every producer immediately, then performs bounded,
// best-effort clearing and closes the connection. It is safe to call repeatedly.
func (c *Client) Close(ctx context.Context) error {
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

func (c *Client) notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) current() (Settings, Snapshot, SessionID, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings, c.desired, c.activeID, c.configRev, c.closed
}

func (c *Client) setStatus(state State, message string) {
	c.mu.Lock()
	c.status.State, c.status.Message = state, message
	c.mu.Unlock()
}

func isIP(host string) bool { return net.ParseIP(host) != nil }
