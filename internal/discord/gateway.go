package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type packet struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

type gatewayEvent struct {
	epoch uint64
	conn  connection
	data  packet
	err   error
}

type assetEvent struct {
	epoch uint64
	key   string
	image string
	err   error
}

type gateway struct {
	client *Client
	deps   dependencies
	events chan gatewayEvent
	images chan assetEvent
	jobs   sync.WaitGroup

	settings    Settings
	configRev   uint64
	epoch       uint64
	conn        connection
	cancel      context.CancelFunc
	dialing     bool
	ready       bool
	hello       bool
	suspended   bool
	attempt     int
	retryAt     time.Time
	deadline    time.Time
	readyAt     time.Time
	identified  time.Time
	interval    time.Duration
	heartbeatAt time.Time
	ackDeadline time.Time
	awaitingACK bool
	session     string
	resumeURL   string
	sequence    *int64

	sends         rollingLimit
	presences     rollingLimit
	lastPresence  time.Time
	lastPayload   []byte
	lastActivity  *activity
	lastSessionID SessionID

	assetCache  map[string]cachedAsset
	assetCancel context.CancelFunc
	assetEpoch  uint64
	assetKey    string
}

func (c *Client) run(ctx context.Context) {
	g := &gateway{
		client: c, deps: c.deps, events: make(chan gatewayEvent, 64), images: make(chan assetEvent, 4),
		sends: rollingLimit{window: time.Minute}, presences: rollingLimit{window: 20 * time.Second},
		assetCache: make(map[string]cachedAsset),
	}
	defer func() {
		g.stopArtwork()
		g.disconnect(true)
		g.jobs.Wait()
		// A canceled dial may already have handed off its socket before the
		// shutdown request. Close these unadopted sockets as well.
		for len(g.events) > 0 {
			if event := <-g.events; event.conn != nil {
				event.conn.Close(false)
			}
		}
		c.mu.Lock()
		c.closed = true
		c.settings.Token = ""
		c.status.State = Closed
		c.mu.Unlock()
		close(c.done)
	}()
	timer := time.NewTimer(c.deps.wakeEvery)
	defer timer.Stop()
	for {
		settings, snapshot, _, revision, closed := c.current()
		if closed || ctx.Err() != nil {
			return
		}
		now := g.deps.now()
		if revision != g.configRev {
			g.configure(settings, revision)
		}
		g.advance(now)
		if !snapshot.Active || (!snapshot.ValidUntil.IsZero() && !now.Before(snapshot.ValidUntil)) {
			g.stopArtwork()
		}
		if g.ready {
			g.publish(now)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		delay := g.deps.wakeEvery
		// A failure can schedule an immediately due retry inside advance. Ensure
		// this loop turns again instead of sleeping until the periodic wake-up.
		if g.conn == nil && !g.dialing && g.settings.Enabled && !g.suspended && !g.retryAt.After(now) {
			delay = 0
		}
		for _, deadline := range []time.Time{g.retryAt, g.deadline, g.heartbeatAt, g.ackDeadline} {
			if !deadline.IsZero() && deadline.After(now) {
				delay = min(delay, deadline.Sub(now))
			}
		}
		timer.Reset(delay)
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-timer.C:
		case event := <-g.events:
			g.receive(event)
		case image := <-g.images:
			if image.epoch == g.assetEpoch && image.key == g.assetKey {
				lifetime := time.Hour
				if image.err != nil {
					lifetime = 5 * time.Minute
				}
				if len(g.assetCache) >= 128 {
					clear(g.assetCache)
				}
				g.assetCache[image.key] = cachedAsset{image: image.image, expires: g.deps.now().Add(lifetime)}
				g.client.mu.Lock()
				g.client.status.ArtworkHTTPStatus = 0
				if image.err == nil && image.image != "" {
					g.client.status.ArtworkState = ArtworkReady
				} else {
					g.client.status.ArtworkState = ArtworkFailed
					var conversion artworkError
					if errors.As(image.err, &conversion) {
						g.client.status.ArtworkHTTPStatus = conversion.status
					}
				}
				g.client.mu.Unlock()
				g.stopArtwork()
			}
		}
	}
}

func (g *gateway) configure(settings Settings, revision uint64) {
	g.stopArtwork()
	g.disconnect(true)
	g.settings, g.configRev = settings, revision
	g.session, g.resumeURL, g.sequence = "", "", nil
	g.suspended, g.attempt, g.retryAt = false, 0, time.Time{}
	clear(g.assetCache)
	g.client.mu.Lock()
	g.client.status.Username, g.client.status.Message = "", ""
	g.client.status.RetryAt = time.Time{}
	g.client.mu.Unlock()
	switch {
	case !settings.Enabled:
		g.client.setStatus(Disabled, "Sharing is off")
	case settings.Token == "":
		g.suspended = true
		g.client.setStatus(NeedsLogin, "Add a Discord user-session token")
	case !validSettings(settings):
		g.suspended = true
		g.client.setStatus(Failed, "Invalid Discord application ID, asset key, or token length")
	default:
		g.client.setStatus(Connecting, "Connecting to Discord")
	}
}

func validSettings(settings Settings) bool {
	if len(settings.Token) > 2048 || len(settings.ApplicationID) > 20 || len(settings.LargeImage) > 128 {
		return false
	}
	for _, r := range settings.ApplicationID {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (g *gateway) advance(now time.Time) {
	if !g.settings.Enabled || g.suspended {
		return
	}
	if g.conn == nil {
		if !g.dialing && !now.Before(g.retryAt) {
			// A fresh Identify gets its own cooldown, even after invalid sessions
			// and rapid off/on toggles. Resume does not consume this allowance.
			if g.session == "" && now.Before(g.identified.Add(5*time.Second)) {
				g.retryAt = g.identified.Add(5 * time.Second)
				g.client.mu.Lock()
				g.client.status.RetryAt = g.retryAt
				g.client.mu.Unlock()
				g.client.setStatus(RetryWait, "Waiting before authenticating again")
				return
			}
			g.connect()
		}
		return
	}
	if !g.deadline.IsZero() && !now.Before(g.deadline) {
		g.retry("Discord handshake timed out", -1)
		return
	}
	if g.awaitingACK && !now.Before(g.ackDeadline) {
		g.retry("Discord heartbeat timed out", -1)
		return
	}
	if g.hello && !now.Before(g.heartbeatAt) {
		g.heartbeat()
		g.heartbeatAt = now.Add(g.interval)
	}
}

func (g *gateway) connect() {
	settings, _, _, revision, closed := g.client.current()
	if closed || !settings.Enabled || revision != g.configRev {
		return
	}
	g.epoch++
	epoch := g.epoch
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel, g.dialing = cancel, true
	g.retryAt = time.Time{}
	g.client.setStatus(Connecting, "Connecting to Discord")
	endpoint := g.deps.url
	if g.session != "" && g.resumeURL != "" {
		endpoint = g.resumeURL
	}
	g.jobs.Add(1)
	go func() {
		defer g.jobs.Done()
		dialCtx, dialCancel := context.WithTimeout(ctx, g.deps.handshake)
		defer dialCancel()
		conn, err := g.deps.dial(dialCtx, endpoint)
		if ctx.Err() != nil {
			if conn != nil {
				conn.Close(false)
			}
			return
		}
		select {
		case g.events <- gatewayEvent{epoch: epoch, conn: conn, err: err}:
		case <-ctx.Done():
			if conn != nil {
				conn.Close(false)
			}
		}
	}()
}

func (g *gateway) receive(event gatewayEvent) {
	// Configuration can change while an event is waiting in the mailbox. In
	// particular, a queued Hello must not authenticate after disable/sign-out.
	settings, _, _, revision, closed := g.client.current()
	if event.epoch != g.epoch || closed || !settings.Enabled || revision != g.configRev {
		if event.conn != nil {
			event.conn.Close(false)
		}
		return
	}
	if event.err != nil {
		g.connectionError(event.err)
		return
	}
	if event.conn != nil {
		g.conn, g.dialing = event.conn, false
		g.deadline = g.deps.now().Add(g.deps.handshake)
		g.startReader(event.conn, event.epoch)
		return
	}
	p := event.data
	if p.Op == 0 && p.S != nil {
		sequence := *p.S
		g.sequence = &sequence
	}
	switch p.Op {
	case 10:
		var hello struct {
			Interval float64 `json:"heartbeat_interval"`
		}
		if g.hello || json.Unmarshal(p.D, &hello) != nil || !finite(hello.Interval) || hello.Interval <= 0 || hello.Interval > float64((10*time.Minute)/time.Millisecond) {
			g.retry("Invalid Discord Hello", -1)
			return
		}
		g.interval, g.hello = time.Duration(hello.Interval*float64(time.Millisecond)), true
		g.heartbeatAt = g.deps.now().Add(g.deps.jitter(g.interval))
		if g.session != "" && g.sequence != nil {
			g.client.setStatus(Resuming, "Resuming Discord session")
			g.send(6, map[string]any{"token": g.settings.Token, "session_id": g.session, "seq": g.sequence})
		} else {
			g.client.setStatus(Identifying, "Authenticating with Discord")
			g.identified = g.deps.now()
			g.send(2, map[string]any{
				"token":      g.settings.Token,
				"properties": map[string]string{"$os": runtime.GOOS, "$browser": "AnimiX", "$device": "AnimiX"},
				"presence":   map[string]any{"status": "online", "afk": false, "activities": []any{}},
			})
		}
	case 0:
		if !g.hello {
			return
		}
		switch p.T {
		case "READY":
			var ready struct {
				Session string `json:"session_id"`
				URL     string `json:"resume_gateway_url"`
				User    struct {
					Username string `json:"username"`
				} `json:"user"`
			}
			if json.Unmarshal(p.D, &ready) != nil || ready.Session == "" {
				g.retry("Invalid Discord Ready", -1)
				return
			}
			g.session, g.resumeURL = ready.Session, resumeEndpoint(ready.URL)
			g.client.mu.Lock()
			g.client.status.Username = shortText(ready.User.Username, 80)
			g.client.mu.Unlock()
			g.markReady()
		case "RESUMED":
			g.markReady()
		}
	case 1:
		if g.hello {
			g.heartbeat()
		}
	case 11:
		g.awaitingACK, g.ackDeadline = false, time.Time{}
		g.client.mu.Lock()
		g.client.status.LastACK = g.deps.now()
		g.client.mu.Unlock()
	case 7:
		g.retry("Discord requested reconnect", 0)
	case 9:
		var resumable bool
		_ = json.Unmarshal(p.D, &resumable)
		if !resumable {
			g.session, g.resumeURL, g.sequence = "", "", nil
		}
		g.retry("Discord session invalidated", time.Second+g.deps.jitter(4*time.Second))
	}
}

func (g *gateway) startReader(conn connection, epoch uint64) {
	ctx, cancel := context.WithCancel(context.Background())
	previousCancel := g.cancel
	g.cancel = func() { previousCancel(); cancel() }
	g.jobs.Add(1)
	go func() {
		defer g.jobs.Done()
		for {
			data, err := conn.Read(ctx)
			var p packet
			if err == nil {
				err = json.Unmarshal(data, &p)
			}
			select {
			case g.events <- gatewayEvent{epoch: epoch, data: p, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
}

func (g *gateway) markReady() {
	g.ready, g.readyAt, g.deadline = true, g.deps.now(), time.Time{}
	g.lastPayload, g.lastActivity = nil, nil
	g.client.mu.Lock()
	g.client.status.RetryAt = time.Time{}
	g.client.mu.Unlock()
	g.client.setStatus(Ready, "Connected")
}

func (g *gateway) heartbeat() {
	sentAt := g.deps.now()
	if g.send(1, g.sequence) && !g.awaitingACK {
		g.awaitingACK = true
		g.ackDeadline = sentAt.Add(g.interval)
	}
}

func (g *gateway) send(op int, data any) bool {
	payload, err := json.Marshal(struct {
		Op int `json:"op"`
		D  any `json:"d"`
	}{op, data})
	if err != nil {
		return false
	}
	return g.write(payload)
}

func (g *gateway) write(payload []byte) bool {
	settings, _, _, revision, closed := g.client.current()
	if g.conn == nil || closed || !settings.Enabled || revision != g.configRev {
		return false
	}
	if len(payload) > 4096 {
		g.disconnect(false)
		g.suspended = true
		g.client.setStatus(Failed, "Discord payload exceeds the Gateway limit")
		return false
	}
	now := g.deps.now()
	if next := g.sends.next(now, 120); next.After(now) {
		g.retry("Discord send budget exhausted", next.Sub(now))
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.deps.writeLimit)
	defer cancel()
	// Count attempts, including writes that might have reached Discord before
	// failing locally. Reconnecting must not reset this budget.
	g.sends.record(now)
	if err := g.conn.Write(ctx, payload); err != nil {
		g.connectionError(err)
		return false
	}
	return true
}

func (g *gateway) publish(now time.Time) {
	// Re-read the mailbox just before formatting, so a clear/disable that arrived
	// during a network operation takes precedence over this loop's old snapshot.
	settings, snapshot, id, revision, closed := g.client.current()
	if closed || !settings.Enabled || revision != g.configRev {
		return
	}
	image := g.artwork(snapshot, now)
	value := formatActivity(settings, snapshot, image, now)
	if id == g.lastSessionID && value != nil && g.lastActivity != nil && value.Timestamps != nil && g.lastActivity.Timestamps != nil {
		old, next := g.lastActivity.Timestamps, value.Timestamps
		if absolute(old.Start-next.Start) < 2000 && absolute(old.End-next.End) < 2000 {
			value.Timestamps = old // Property notification jitter isn't a seek.
		}
	}
	payload := presencePayload(value)
	if bytes.Equal(payload, g.lastPayload) {
		return
	}
	maximum := 4 // Reserve the fifth presence slot for clearing.
	if value == nil {
		maximum = 5
	} else if now.Before(g.lastPresence.Add(5 * time.Second)) {
		return
	}
	if g.presences.next(now, maximum).After(now) {
		return
	}
	g.presences.record(now)
	if g.write(payload) {
		g.lastPayload, g.lastActivity, g.lastSessionID = payload, value, id
		g.lastPresence = now
		g.client.mu.Lock()
		g.client.status.LastSent = now
		g.client.mu.Unlock()
	}
}

func absolute(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func (g *gateway) connectionError(err error) {
	code := int(websocket.CloseStatus(err))
	g.client.mu.Lock()
	g.client.status.CloseCode = code
	g.client.mu.Unlock()
	switch code {
	case 4004:
		g.disconnect(false)
		g.suspended = true
		g.client.setStatus(NeedsLogin, "Discord rejected the token; replace it to reconnect")
	case 4001, 4002, 4010, 4011, 4012, 4013, 4014:
		g.disconnect(false)
		g.suspended = true
		g.client.setStatus(Failed, "Discord rejected the Gateway configuration")
	default:
		if code == 1000 || code == 1001 || code == 4003 || code == 4005 || code == 4007 || code == 4009 {
			g.session, g.resumeURL, g.sequence = "", "", nil
		}
		delay := time.Duration(-1)
		if code == 4008 {
			delay = 20 * time.Second
		}
		g.retry("Discord connection interrupted", delay)
	}
}

func (g *gateway) retry(message string, delay time.Duration) {
	if !g.readyAt.IsZero() && g.deps.now().Sub(g.readyAt) >= time.Minute {
		g.attempt = 0
	}
	g.disconnect(false)
	if delay < 0 {
		delay = g.deps.retry(g.attempt)
	}
	g.attempt++
	g.retryAt = g.deps.now().Add(delay)
	g.client.mu.Lock()
	g.client.status.RetryAt = g.retryAt
	g.client.mu.Unlock()
	g.client.setStatus(RetryWait, message)
}

func (g *gateway) disconnect(normal bool) {
	conn := g.conn
	if conn != nil && normal && g.ready {
		now := g.deps.now()
		if !g.presences.next(now, 5).After(now) && !g.sends.next(now, 120).After(now) {
			ctx, cancel := context.WithTimeout(context.Background(), g.deps.writeLimit)
			g.presences.record(now)
			g.sends.record(now)
			_ = conn.Write(ctx, presencePayload(nil))
			cancel()
		}
	}
	g.epoch++ // Invalidate callbacks before touching the socket.
	if conn != nil {
		conn.Close(normal)
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.conn, g.cancel = nil, nil
	g.dialing, g.ready, g.hello, g.awaitingACK = false, false, false, false
	g.deadline, g.heartbeatAt, g.ackDeadline, g.readyAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	g.lastPayload, g.lastActivity = nil, nil
}

func resumeEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wss" || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "gateway.discord.gg" && !strings.HasSuffix(host, ".discord.gg") {
		return ""
	}
	u.RawQuery = "v=9&encoding=json"
	return u.String()
}

func (g *gateway) artwork(snapshot Snapshot, now time.Time) string {
	if !snapshot.Active || (!snapshot.ValidUntil.IsZero() && !now.Before(snapshot.ValidUntil)) || !g.settings.ShowArtwork || g.settings.ApplicationID == "" || !publicImage(snapshot.CoverURL) {
		g.stopArtwork()
		return ""
	}
	key := g.settings.ApplicationID + ":" + snapshot.CoverURL
	if cached, ok := g.assetCache[key]; ok && now.Before(cached.expires) {
		if cached.image != "" {
			g.client.mu.Lock()
			g.client.status.ArtworkState = ArtworkReady
			g.client.mu.Unlock()
		}
		return cached.image
	}
	if key == g.assetKey {
		return ""
	}
	g.stopArtwork()
	g.assetEpoch++
	epoch := g.assetEpoch
	ctx, cancel := context.WithCancel(context.Background())
	g.assetKey, g.assetCancel = key, cancel
	g.client.mu.Lock()
	g.client.status.ArtworkState, g.client.status.ArtworkHTTPStatus = ArtworkLoading, 0
	g.client.mu.Unlock()
	settings := g.settings
	g.jobs.Add(1)
	go func() {
		defer g.jobs.Done()
		defer cancel()
		requestCtx, requestCancel := context.WithTimeout(ctx, 5*time.Second)
		defer requestCancel()
		image, err := g.deps.asset(requestCtx, settings, snapshot.CoverURL)
		select {
		case g.images <- assetEvent{epoch: epoch, key: key, image: image, err: err}:
		case <-ctx.Done():
		}
	}()
	return ""
}

func (g *gateway) stopArtwork() {
	g.assetEpoch++
	if g.assetCancel != nil {
		g.assetCancel()
	}
	g.assetKey, g.assetCancel = "", nil
}
