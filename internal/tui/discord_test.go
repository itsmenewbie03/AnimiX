package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/discord"
	"github.com/itsmenewbie03/animix/internal/mangareader"
)

type fakeDiscord struct {
	settings discord.Settings
	status   discord.Status
	next     discord.SessionID
	updates  []discord.Snapshot
	ended    []discord.SessionID
}

func (f *fakeDiscord) Configure(settings discord.Settings) {
	f.settings = settings
	f.status.Enabled, f.status.HasToken = settings.Enabled, settings.Token != ""
	f.status.HasApplicationID = settings.ApplicationID != ""
	f.status.ShowBrowsing, f.status.ShowArtwork = settings.ShowBrowsing, settings.ShowArtwork
	if settings.ShowArtwork {
		f.status.ArtworkState = discord.ArtworkWaiting
	} else {
		f.status.ArtworkState = discord.ArtworkOff
	}
	if settings.Enabled {
		f.status.State = discord.Connecting
	} else {
		f.status.State = discord.Disabled
	}
}

func (f *fakeDiscord) Begin(discord.Snapshot) discord.SessionID { f.next++; return f.next }
func (f *fakeDiscord) Update(_ discord.SessionID, snapshot discord.Snapshot) {
	f.updates = append(f.updates, snapshot)
}
func (f *fakeDiscord) End(id discord.SessionID) { f.ended = append(f.ended, id) }
func (f *fakeDiscord) Status() discord.Status   { return f.status }

func discordKey(model Model, value string) Model {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	if value == "enter" {
		key = tea.KeyMsg{Type: tea.KeyEnter}
	}
	updated, _ := model.handleKey(key)
	return updated.(Model)
}

func TestDiscordTokenIsMaskedAndSavingDoesNotEnableSharing(t *testing.T) {
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.toml"))
	presence := &fakeDiscord{}
	model := New(nil, store, "", "mpv", WithDiscordPresence(presence))
	model.loading = false
	model = discordKey(model, "p")
	model = discordKey(model, "t")
	if model.stage != stageDiscordToken || !model.acceptsTextInput() {
		t.Fatal("token shortcut did not open a text input")
	}
	model.discordInput.SetValue("private-fixture-token")
	if strings.Contains(model.View(), "private-fixture-token") {
		t.Fatal("Discord token is visible")
	}
	model = discordKey(model, "enter")
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.DiscordToken != "private-fixture-token" || config.DiscordPreferences().Enabled || presence.settings.Enabled {
		t.Fatal("saving token unexpectedly enabled sharing or lost the credential")
	}
	if model.discordInput.Value() != "" {
		t.Fatal("token input retained the secret after submission")
	}
	model = discordKey(model, "e")
	config, err = store.Load()
	if err != nil || !config.DiscordPreferences().Enabled || !presence.settings.Enabled {
		t.Fatalf("explicit opt-in did not connect: %v", err)
	}
	model = discordKey(model, "x")
	config, err = store.Load()
	if err != nil || config.DiscordToken != "" || presence.settings.Enabled {
		t.Fatalf("sign-out did not clear credential and stop sharing: %v", err)
	}
}

func TestDiscordStopsSharingEvenWhenSignOutCannotBeSaved(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	presence := &fakeDiscord{}
	presence.Configure(discord.Settings{Enabled: true, Token: "fixture"})
	model := New(nil, appconfig.NewStore(filepath.Join(blocked, "config.toml")), "", "mpv", WithDiscordPresence(presence))
	model.loading, model.stage = false, stageDiscord
	model = discordKey(model, "x")
	if presence.settings.Enabled || model.err == nil {
		t.Fatal("failed persistence silently kept sharing or hid the save error")
	}
}

func TestDiscordArtworkUsesShippedApplication(t *testing.T) {
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.toml"))
	presence := &fakeDiscord{}
	model := New(nil, store, "", "mpv", WithDiscordPresence(presence))
	model.loading, model.stage = false, stageDiscord
	model = discordKey(model, "a")
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.DiscordPreferences().ShowArtwork || !presence.settings.ShowArtwork {
		t.Fatal("poster artwork toggle was not persisted and applied")
	}
	if presence.settings.ApplicationID != discord.DefaultApplicationID {
		t.Fatalf("application ID = %q", presence.settings.ApplicationID)
	}
	if strings.Contains(model.discordView(), "needs application_id") {
		t.Fatal("artwork screen requested an application ID from the user")
	}
}

func TestReaderPresenceUsesReceiptTimeAndIgnoresOldSession(t *testing.T) {
	presence := &fakeDiscord{}
	model := New(nil, nil, "", "mpv", WithDiscordPresence(presence))
	entry := appconfig.MangaReadingEntry{Title: "New chapter", ChapterNumber: "4", Page: 3, TotalPages: 10}
	model.activeMangaSessionID = "current"
	model.beginReaderPresence("current", entry)
	seen := time.Now().Add(-30 * time.Second)
	model.updateReaderPresence(mangareader.ProgressEvent{SessionID: "old", Visible: true, SeenAt: seen}, entry)
	if len(presence.updates) != 0 {
		t.Fatal("old reader updated foreground presence")
	}
	model.updateReaderPresence(mangareader.ProgressEvent{SessionID: "current", Visible: true, SeenAt: seen}, entry)
	snapshot := presence.updates[0]
	if !snapshot.Active || !snapshot.ObservedAt.Equal(seen) || !snapshot.ValidUntil.Equal(seen.Add(time.Minute)) {
		t.Fatalf("reader lease was refreshed at TUI delivery time: %#v", snapshot)
	}
	model.endReaderPresence("current")
	if len(presence.ended) != 1 || presence.ended[0] != 1 {
		t.Fatal("reader cleanup did not end its presence session")
	}
}

func TestPlayerExecFailureEndsPresenceSession(t *testing.T) {
	presence := &fakeDiscord{}
	execution := &observedPlayback{
		ctx: context.Background(), executable: "animix-nonexistent-player-fixture",
		video: catalog.Video{URL: "fixture"}, presence: presence,
		metadata: discord.Snapshot{Kind: discord.Watching, Title: "Fixture"},
	}
	if err := execution.Run(); err == nil {
		t.Fatal("missing player unexpectedly ran")
	}
	if presence.next != 1 || len(presence.ended) != 1 || presence.ended[0] != 1 || len(presence.updates) != 0 {
		t.Fatal("failed process left an active presence producer")
	}
}

func TestPresenceCoverPrefersAniListCDNOverSourcePoster(t *testing.T) {
	model := New(nil, nil, "", "mpv")
	model.trackingEntries = []anilist.Entry{{MediaID: 42, CoverURL: "https://s4.anilist.co/poster.jpg"}}
	if got := model.presenceCover(42, "https://source.example/blocked.webp"); got != "https://s4.anilist.co/poster.jpg" {
		t.Fatalf("presence cover = %q", got)
	}
	if got := model.presenceCover(7, "https://source.example/fallback.webp"); got != "https://source.example/fallback.webp" {
		t.Fatalf("unlinked presence cover = %q", got)
	}
}
