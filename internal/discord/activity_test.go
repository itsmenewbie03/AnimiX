package discord

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlaybackTimestampsAccountForDelaySpeedAndUnknownDuration(t *testing.T) {
	observed := time.Unix(1_800_000_000, 0)
	snapshot := Snapshot{Active: true, Kind: Watching, Position: 100, Duration: 1000, Rate: 2, ObservedAt: observed}
	now := observed.Add(10 * time.Second)
	got := formatActivity(Settings{}, snapshot, "", now)
	if got.Timestamps.Start != observed.Add(-50*time.Second).UnixMilli() || got.Timestamps.End != observed.Add(450*time.Second).UnixMilli() {
		t.Fatalf("delayed 2x timestamps = %#v", got.Timestamps)
	}
	for _, mode := range []string{"paused", "buffering"} {
		copy := snapshot
		copy.Paused, copy.Buffering = mode == "paused", mode == "buffering"
		if value := formatActivity(Settings{}, copy, "", now); value.Timestamps != nil {
			t.Fatalf("%s activity has running timestamps", mode)
		}
	}
	snapshot.Duration = 0
	if value := formatActivity(Settings{}, snapshot, "", now); value.Timestamps.End != 0 {
		t.Fatal("unknown duration invented an end time")
	}
	snapshot.Rate = 0
	if value := formatActivity(Settings{}, snapshot, "", now); value.Timestamps != nil {
		t.Fatal("invalid playback rate produced timestamps")
	}
}

func TestReaderLeaseExpiresAndBrowsingIsOptIn(t *testing.T) {
	now := time.Now()
	snapshot := Snapshot{Kind: Reading, Active: true, Title: "Manga", Number: "12", Page: 3, Pages: 20, ValidUntil: now.Add(time.Minute)}
	got := formatActivity(Settings{}, snapshot, "", now)
	if got.State != "Reading · Chapter 12 · Page 3/20" || got.Timestamps != nil {
		t.Fatalf("reader activity = %#v", got)
	}
	if value := formatActivity(Settings{}, snapshot, "", now.Add(time.Minute)); value != nil {
		t.Fatal("expired tab still advertises reading")
	}
	if value := formatActivity(Settings{ShowBrowsing: true}, Snapshot{}, "", now); value == nil || value.Details != "Browsing AnimiX" {
		t.Fatal("opted-in browsing fallback missing")
	}
}

func TestPayloadUsesOnlyPublicMetadataAndFitsGatewayLimit(t *testing.T) {
	snapshot := watching(strings.Repeat("界", 1000))
	snapshot.Part, snapshot.Number = strings.Repeat("界", 1000), ""
	snapshot.AniListMediaID = 123
	value := formatActivity(Settings{ApplicationID: "456"}, snapshot, "mp:"+strings.Repeat("a", 2048), time.Now())
	payload := presencePayload(value)
	if len(payload) > 4096 || !json.Valid(payload) {
		t.Fatalf("payload length = %d, valid = %v", len(payload), json.Valid(payload))
	}
	if value.Metadata.URLs[0] != "https://anilist.co/anime/123" {
		t.Fatalf("button = %#v", value.Metadata)
	}
	if value.Assets.LargeURL != "https://anilist.co/anime/123" {
		t.Fatalf("poster URL = %q", value.Assets.LargeURL)
	}
	snapshot.AniListMediaID = 0
	if got := formatActivity(Settings{ApplicationID: "456"}, snapshot, "mp:poster", time.Now()); len(got.Buttons) != 0 || got.Assets.LargeURL != "" {
		t.Fatal("unlinked source media received a fabricated AniList button")
	}
}

func TestOversizedEscapedArtworkFallsBackToText(t *testing.T) {
	snapshot := watching(strings.Repeat("<", 128))
	snapshot.Number, snapshot.Part = "", strings.Repeat(">", 128)
	value := formatActivity(Settings{ApplicationID: "123"}, snapshot, "mp:"+strings.Repeat("x", 2048), time.Now())
	payload := presencePayload(value)
	var p packet
	if len(payload) > 4096 || json.Unmarshal(payload, &p) != nil {
		t.Fatalf("fallback payload exceeds Gateway limits: %d bytes", len(payload))
	}
	got := activities(t, p)
	if len(got) != 1 || got[0].Details != snapshot.Title || got[0].Assets != nil {
		t.Fatalf("text fallback = %#v", got)
	}
	if value.Assets == nil {
		t.Fatal("encoding mutated the retained activity")
	}
}

func TestRollingBudgetAndClearReservation(t *testing.T) {
	now := time.Now()
	limit := rollingLimit{window: 20 * time.Second}
	for index := 0; index < 4; index++ {
		at := now.Add(time.Duration(index) * 5 * time.Second)
		if limit.next(at, 4).After(at) {
			t.Fatal("normal paced update was blocked")
		}
		limit.record(at)
	}
	at := now.Add(16 * time.Second)
	if !limit.next(at, 4).After(at) || limit.next(at, 5).After(at) {
		t.Fatal("fifth slot was not reserved for a clear")
	}
	limit.record(at)
	if got := limit.next(at, 5); !got.Equal(now.Add(20 * time.Second)) {
		t.Fatalf("next clear = %v", got)
	}
}

func TestContinuousChangesCoalesceWithoutTrailingDebounce(t *testing.T) {
	now := time.Now()
	conn := newConnection()
	c := &Client{wake: make(chan struct{}, 1), settings: Settings{Enabled: true}, configRev: 1}
	g := &gateway{
		client: c, settings: c.settings, configRev: 1, conn: conn, ready: true,
		deps:  dependencies{now: func() time.Time { return now }, writeLimit: time.Second},
		sends: rollingLimit{window: time.Minute}, presences: rollingLimit{window: 20 * time.Second},
	}
	id := c.Begin(watching("Initial"))
	var sends []time.Time
	for index := 0; index < 500; index++ {
		now = now.Add(100 * time.Millisecond)
		snapshot := watching("Page " + strings.Repeat("a", index%100))
		c.Update(id, snapshot)
		g.publish(now)
		if len(conn.out) > 0 {
			<-conn.out
			sends = append(sends, now)
		}
	}
	if len(sends) < 8 {
		t.Fatalf("continuous input starved updates: %d sends", len(sends))
	}
	for _, end := range sends {
		count := 0
		for _, at := range sends {
			if at.After(end.Add(-20*time.Second)) && !at.After(end) {
				count++
			}
		}
		if count > 4 {
			t.Fatalf("ordinary updates consumed clear reservation: %d", count)
		}
	}
	c.End(id)
	g.publish(now)
	if got := activities(t, conn.next(t, 3)); len(got) != 0 {
		t.Fatal("clear did not replace queued updates")
	}
}

func TestResumeEndpointCannotRedirectCredentials(t *testing.T) {
	for _, raw := range []string{"wss://example.com", "ws://gateway.discord.gg", "wss://gateway.discord.gg.evil.example", "wss://user:password@gateway.discord.gg"} {
		if resumeEndpoint(raw) != "" {
			t.Fatalf("accepted resume endpoint %q", raw)
		}
	}
	if got := resumeEndpoint("wss://gateway-us-east1-b.discord.gg"); got != "wss://gateway-us-east1-b.discord.gg?v=9&encoding=json" {
		t.Fatalf("resume endpoint = %q", got)
	}
}
