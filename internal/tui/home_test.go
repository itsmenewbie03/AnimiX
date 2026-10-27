package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/aniskip"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/player"
	"github.com/itsmenewbie03/animix/internal/repository"
	appupdate "github.com/itsmenewbie03/animix/internal/update"
)

type fakeUpdateService struct {
	release   appupdate.Release
	installed bool
}

func (service *fakeUpdateService) Check(context.Context, string) (appupdate.Release, error) {
	return service.release, nil
}

func (service *fakeUpdateService) Install(context.Context, appupdate.Release) error {
	service.installed = true
	return nil
}

func TestUpdateCheckPromptsAndInstalls(t *testing.T) {
	t.Parallel()
	service := &fakeUpdateService{release: appupdate.Release{Version: "v0.2.0"}}
	model := New(nil, nil, "", "mpv", WithUpdateCheck(service, "v0.1.0"))
	model.loading = false

	message := model.checkForUpdates()()
	updated, _ := model.Update(message)
	model = updated.(Model)
	if model.stage != stageUpdatePrompt || !strings.Contains(model.View(), "v0.2.0") {
		t.Fatalf("update prompt not shown: stage=%d\n%s", model.stage, model.View())
	}

	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(Model)
	if command == nil || !model.loading {
		t.Fatal("update installer was not started")
	}
	message = model.installUpdate()()
	updated, _ = model.Update(message)
	model = updated.(Model)
	if !service.installed || model.err != nil || !strings.Contains(model.status, "restart") {
		t.Fatalf("update result = installed:%t err:%v status:%q", service.installed, model.err, model.status)
	}
}

func TestUpdatePromptCanBeSkipped(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.loading = false
	model.stage = stageUpdatePrompt
	model.updateReturn = stageSources
	model.availableUpdate = appupdate.Release{Version: "v0.2.0"}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	if model.stage != stageSources || model.availableUpdate.Version != "" {
		t.Fatalf("skipped update state = %#v", model)
	}
}

func TestNewEpisodeEntriesMatchAnymeXHomeRules(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.Local)
	daysFromNow := func(days int) int64 { return now.Add(time.Duration(days) * 24 * time.Hour).Unix() }
	endDate := func(daysAgo int) *time.Time {
		value := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		return &value
	}
	model := Model{trackingEntries: []anilist.Entry{
		{MediaID: 1, Title: "Newest airing", Type: "ANIME", Status: "CURRENT", MediaStatus: "RELEASING", Progress: 3, Available: 7, NextAiringEpisode: &anilist.NextAiringEpisode{Episode: 8, AiringAt: daysFromNow(6)}},
		{MediaID: 2, Title: "Recent finished", Type: "ANIME", Status: "REPEATING", MediaStatus: "FINISHED", Progress: 10, Available: 12, EndDate: endDate(2)},
		{MediaID: 3, Title: "Older airing", Type: "ANIME", Status: "WATCHING", MediaStatus: "AIRING", Progress: 2, Available: 5, NextAiringEpisode: &anilist.NextAiringEpisode{Episode: 6, AiringAt: daysFromNow(2)}},
		{MediaID: 4, Title: "Old finished", Type: "ANIME", Status: "CURRENT", MediaStatus: "FINISHED", Progress: 10, Available: 12, EndDate: endDate(8)},
		{MediaID: 5, Title: "Paused", Type: "ANIME", Status: "PAUSED", MediaStatus: "RELEASING", Progress: 1, Available: 2},
		{MediaID: 6, Title: "Caught up", Type: "ANIME", Status: "CURRENT", MediaStatus: "RELEASING", Progress: 4, Available: 4},
		{MediaID: 7, Title: "Manga", Type: "MANGA", Status: "CURRENT", Progress: 1, Available: 20},
	}}

	entries := model.newEpisodeEntries(now)
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	wantOrder := []string{"Newest airing", "Recent finished", "Older airing"}
	for index, want := range wantOrder {
		if entries[index].entry.Title != want {
			t.Fatalf("entry %d = %q, want %q", index, entries[index].entry.Title, want)
		}
	}
}

func TestNewEpisodeResolutionSelectsMatchAndNextEpisode(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	entry := anilist.Entry{MediaID: 42, Title: "My Anime", Progress: 2, Available: 4}
	model.resolvingEpisode = &entry
	model.resolutionQuery = "My Anime"
	model.selectedSource = catalog.Source{ID: "provider", Name: "Provider", Type: "anime"}
	model.continueWatching = []appconfig.ContinueEntry{{SourceID: "provider", MediaURL: "/my-anime", WatchedEpisodes: []string{"3"}}}

	updated, command := model.Update(searchMsg{pages: catalog.Pages{List: []catalog.Media{
		{Title: "Wrong Show"},
		{Title: "My Anime", URL: "/my-anime"},
	}}})
	model = updated.(Model)
	if command == nil || model.selectedMedia.Title != "My Anime" || !model.loading {
		t.Fatalf("automatic match state = %#v", model)
	}

	updated, _ = model.Update(detailsMsg{media: catalog.Media{Title: "My Anime", URL: "/my-anime", Episodes: []catalog.Episode{
		{Name: "Episode 1", EpisodeNumber: "1"},
		{Name: "Episode 4", EpisodeNumber: "4"},
		{Name: "Episode 3", EpisodeNumber: "3"},
	}}})
	model = updated.(Model)
	if model.stage != stageEpisodes || model.cursor != 1 {
		t.Fatalf("episode selection = stage %d, cursor %d", model.stage, model.cursor)
	}
	first := model.episodeList.Items()[0].(episodeListItem)
	if !first.aniListWatched || first.localWatched {
		t.Fatalf("AniList watched item = %#v", first)
	}
	third := model.episodeList.Items()[2].(episodeListItem)
	if !third.localWatched {
		t.Fatalf("locally watched item = %#v", third)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.MediaMappings) != 1 || config.MediaMappings[0].Title != "My Anime" {
		t.Fatalf("saved mapping = %#v", config.MediaMappings)
	}
}

func TestManualTitleOverrideStillWaitsForResultSelection(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	entry := anilist.Entry{MediaID: 42, Title: "Expected"}
	model.resolvingEpisode = &entry
	model.resolutionQuery = "Alternative"
	model.manualMapping = true
	model.selectedSource = catalog.Source{Name: "Provider"}

	updated, command := model.Update(searchMsg{pages: catalog.Pages{List: []catalog.Media{{Title: "Candidate"}}}})
	model = updated.(Model)
	if command != nil || model.stage != stageResults || model.loading || model.status != "Select the correct match from Provider" {
		t.Fatalf("manual override state = %#v", model)
	}
}

func TestSourceForMediaPrefersStickyThenActiveSource(t *testing.T) {
	t.Parallel()
	sources := []catalog.Source{{ID: "first", Type: "anime"}, {ID: "active", Type: "anime"}, {ID: "sticky", Type: "anime"}}
	model := Model{
		activeSources: map[string]string{"anime": "active"},
		mediaMappings: []appconfig.MediaMapping{{MediaID: 7, SourceID: "sticky", Title: "Mapped"}},
	}
	if got := model.sourceForMedia(7, sources); got.ID != "sticky" {
		t.Fatalf("sticky source = %q", got.ID)
	}
	if got := model.sourceForMedia(8, sources); got.ID != "active" {
		t.Fatalf("active source = %q", got.ID)
	}
}

func TestHomeTabKeysAndNewEpisodeSelection(t *testing.T) {
	t.Parallel()
	now := time.Now()
	model := New(nil, nil, "", "mpv")
	model.loading = false
	model.width = 100
	model.sources = []catalog.Source{
		{ID: "anime", Name: "Anime source", Type: "anime"},
		{ID: "manga", Name: "Manga source", Type: "manga"},
	}
	model.trackingEntries = []anilist.Entry{{
		MediaID: 1, Title: "Selectable show", Type: "ANIME", Status: "CURRENT",
		MediaStatus: "RELEASING", Progress: 2, Available: 4,
		NextAiringEpisode: &anilist.NextAiringEpisode{Episode: 5, AiringAt: now.Add(24 * time.Hour).Unix()},
	}}
	model.refreshNewEpisodes(now)
	model.homeFocus = homeFocusNewEpisodes

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = updated.(Model)
	if model.homeType != "manga" || model.homeFocus != homeFocusNone {
		t.Fatalf("l tab switch = type %q, focus %d", model.homeType, model.homeFocus)
	}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(Model)
	if model.homeType != "anime" || model.homeFocus != homeFocusNewEpisodes {
		t.Fatalf("h tab switch = type %q, focus %d", model.homeType, model.homeFocus)
	}

	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || !model.loading || model.selectedSource.ID != "anime" || model.search.Value() != "Selectable show" {
		t.Fatalf("selection did not start source search: %#v", model)
	}
}

func TestContinueWatchingIsSelectableFromHome(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.loading = false
	model.sources = []catalog.Source{{ID: "source", Name: "Provider", Type: "anime"}}
	model.trackingEntries = []anilist.Entry{
		{MediaID: 1, Title: "Watching show", Type: "ANIME", Status: "CURRENT", Progress: 3, Total: 12},
		{MediaID: 4, Title: "Older watching show", Type: "ANIME", Status: "CURRENT", Progress: 1, Total: 10},
		{MediaID: 2, Title: "Paused show", Type: "ANIME", Status: "PAUSED", Progress: 2, Total: 12},
		{MediaID: 3, Title: "Reading manga", Type: "MANGA", Status: "CURRENT", Progress: 4, Total: 20},
	}
	model.refreshContinueWatching()
	if len(model.continueWatchingList.Items()) != 2 {
		t.Fatalf("Continue Watching items = %d", len(model.continueWatchingList.Items()))
	}
	first := model.continueWatchingList.Items()[0].(continueWatchingListItem)
	if first.entry.Title != "Watching show" {
		t.Fatalf("first Continue Watching item = %q", first.entry.Title)
	}
	if model.homeFocus != homeFocusContinueWatching {
		t.Fatalf("home focus = %d", model.homeFocus)
	}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.resolvingEpisode == nil || model.resolvingEpisode.Title != "Watching show" {
		t.Fatal("enter did not begin Continue Watching source mapping")
	}
}

func TestNextEpisodeTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		entry anilist.Entry
		want  int
	}{
		{entry: anilist.Entry{Progress: 3, Available: 5, Total: 12}, want: 4},
		{entry: anilist.Entry{Progress: 5, Available: 5, Total: 12}, want: 6},
		{entry: anilist.Entry{Progress: 12, Available: 12, Total: 12}, want: 12},
	}
	for _, test := range tests {
		if got := nextEpisodeTarget(test.entry); got != test.want {
			t.Fatalf("nextEpisodeTarget(%#v) = %d, want %d", test.entry, got, test.want)
		}
	}
}

func TestAniListPlaybackTarget(t *testing.T) {
	t.Parallel()
	base := appconfig.ContinueEntry{
		EpisodeNumber: "4", AniListEntryID: 10, AniListMediaID: 99,
		AniListProgress: 3, AniListTotal: 12, AniListStatus: "PAUSED", AniListMediaStatus: "RELEASING",
	}

	if _, ok := aniListPlaybackTarget(base, player.Progress{Position: 89, Duration: 100}, 3); ok {
		t.Fatal("playback below threshold updated AniList")
	}
	target, ok := aniListPlaybackTarget(base, player.Progress{Position: 90, Duration: 100}, 3)
	if !ok || target.ID != 10 || target.MediaID != 99 || target.Progress != 4 || target.Status != "CURRENT" {
		t.Fatalf("threshold target = %#v, ok = %v", target, ok)
	}
	if _, ok := aniListPlaybackTarget(base, player.Progress{Position: 100, Duration: 100}, 5); ok {
		t.Fatal("AniList progress was allowed to decrease")
	}

	final := base
	final.EpisodeNumber = "12"
	final.AniListProgress = 11
	final.AniListMediaStatus = "FINISHED"
	target, ok = aniListPlaybackTarget(final, player.Progress{Position: 95, Duration: 100}, 11)
	if !ok || target.Progress != 12 || target.Status != "COMPLETED" {
		t.Fatalf("final episode target = %#v, ok = %v", target, ok)
	}

	final.AniListStatus = "REPEATING"
	target, ok = aniListPlaybackTarget(final, player.Progress{Position: 95, Duration: 100}, 11)
	if !ok || target.Status != "REPEATING" {
		t.Fatalf("repeating target = %#v, ok = %v", target, ok)
	}
}

func TestCompletedPlaybackRecordsLocallyWatchedEpisode(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	playback := appconfig.ContinueEntry{SourceID: "source", MediaURL: "media", EpisodeNumber: "5"}

	updated, command := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 90, Duration: 100},
	})
	model = updated.(Model)
	if command != nil {
		t.Fatal("unlinked playback unexpectedly started AniList sync")
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ContinueWatching) != 1 || len(loaded.ContinueWatching[0].WatchedEpisodes) != 1 || loaded.ContinueWatching[0].WatchedEpisodes[0] != "5" {
		t.Fatalf("locally watched playback = %#v", loaded.ContinueWatching)
	}
}

func TestPlaybackReturnsToEpisodeSelector(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.width, model.height = 100, 40
	model.stage = stageStreams
	model.selectedSource = catalog.Source{ID: "source", Name: "Source", Type: "anime"}
	model.details = catalog.Media{
		Title: "Series", URL: "/series",
		Episodes: []catalog.Episode{
			{Name: "Episode 1", EpisodeNumber: "1"},
			{Name: "Episode 2", EpisodeNumber: "2"},
		},
	}
	model.setEpisodeItems()
	model.episodeList.Select(1)
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "/series", EpisodeNumber: "2",
	}

	updated, _ := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 95, Duration: 100},
	})
	model = updated.(Model)
	if model.stage != stageEpisodes {
		t.Fatalf("stage = %d, want episodes (%d)", model.stage, stageEpisodes)
	}
	if model.episodeList.Index() != 1 {
		t.Fatalf("episode cursor = %d, want 1", model.episodeList.Index())
	}
	selected, ok := model.episodeList.SelectedItem().(episodeListItem)
	if !ok || !selected.localWatched {
		t.Fatalf("returned episode = %#v", selected)
	}
}

func TestEarlyPlaybackStopRequestsCloudflareCredentials(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.width, model.height = 100, 40
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5", VideoURL: "https://example.test/video.m3u8",
	}

	updated, command := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 61, Duration: 100, Reason: "eof"},
	})
	model = updated.(Model)
	if command == nil {
		t.Fatal("early end-of-file did not open Cloudflare recovery")
	}
	if model.stage != stageCloudflareCookie || model.cloudflareOperation != cloudflarePlayback {
		t.Fatalf("recovery state = stage %d operation %d", model.stage, model.cloudflareOperation)
	}
	if model.pendingPlayback == nil || model.pendingPlayback.Position != 61 {
		t.Fatalf("pending playback = %#v, want position 61", model.pendingPlayback)
	}
	if !strings.Contains(model.status, "Cloudflare credentials") {
		t.Fatalf("status = %q, want Cloudflare recovery message", model.status)
	}
}

func TestProxyFailureRequestsCloudflareCredentialsAtSavedPosition(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5",
		VideoURL: "http://localhost:44251/m3u8?url=upstream",
	}

	updated, command := model.Update(playerFinishedMsg{
		err:      fmt.Errorf("exit status 2"),
		entry:    &playback,
		progress: &player.Progress{Position: 61, Duration: 100, Reason: "proxy-error", ProxyFailure: true},
	})
	model = updated.(Model)

	if command == nil || model.stage != stageCloudflareCookie {
		t.Fatalf("Cloudflare recovery not opened: stage=%d command=%v", model.stage, command)
	}
	if model.pendingPlayback == nil || model.pendingPlayback.Position != 61 {
		t.Fatalf("pending playback = %#v", model.pendingPlayback)
	}
}

func TestResumePlaybackRefreshesExpiredLocalProxyURL(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.sources = []catalog.Source{{ID: "source", Name: "AnimePahe", Type: "anime"}}
	model.trackingEntries = []anilist.Entry{{ID: 10, MediaID: 99, MALID: 12345, Title: "Overgeared"}}
	entry := appconfig.ContinueEntry{
		Title: "Overgeared", SourceID: "source", SourceName: "AnimePahe", MediaURL: "media",
		EpisodeURL: "episode-5", EpisodeName: "Episode 5", EpisodeNumber: "5",
		VideoURL: "http://localhost:44251/m3u8?url=expired", Quality: "1080p",
		Position: 872.45, Duration: 1419.96,
		AniListEntryID: 10, AniListMediaID: 99,
	}

	updated, command := model.resumePlayback(entry)
	model = updated.(Model)

	if command == nil {
		t.Fatal("resume did not schedule fresh stream resolution")
	}
	if model.pendingPlayback == nil || model.pendingPlayback.Position != entry.Position {
		t.Fatalf("pending playback = %#v", model.pendingPlayback)
	}
	if model.selectedEpisode.URL != entry.EpisodeURL {
		t.Fatalf("selected episode = %#v", model.selectedEpisode)
	}
	if model.resolvingEpisode == nil || model.resolvingEpisode.MediaID != 99 || model.malID != 12345 {
		t.Fatalf("AniSkip context = entry %#v, MAL ID %d", model.resolvingEpisode, model.malID)
	}
	if model.pendingPlayback.MALID != 12345 {
		t.Fatalf("pending MAL ID = %d, want 12345", model.pendingPlayback.MALID)
	}
	if model.status != "" {
		t.Fatalf("status = %q, want no layout-shifting status", model.status)
	}
}

func TestAniListLibraryEnrichesSavedPlaybackForAniSkip(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.continueWatching = []appconfig.ContinueEntry{{AniListMediaID: 191656, EpisodeNumber: "4"}}
	model.trackingEntries = []anilist.Entry{{MediaID: 191656, MALID: 12345}}

	model.enrichPlaybackLinks()

	if got := model.continueWatching[0].MALID; got != 12345 {
		t.Fatalf("MAL ID = %d, want 12345", got)
	}
}

func TestResolvedResumeReplacesExpiredProxyURL(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	entry := appconfig.ContinueEntry{
		VideoURL: "http://localhost:44251/m3u8?url=expired", Quality: "1080p",
		Position: 872.45, Duration: 1419.96,
	}
	model.pendingPlayback = &entry

	updated, command := model.Update(videosMsg{items: []catalog.Video{{
		URL: "http://localhost:55321/m3u8?url=fresh", Quality: "1080p",
	}}})
	model = updated.(Model)

	if command == nil {
		t.Fatal("fresh stream was not launched")
	}
	if model.pendingPlayback != nil {
		t.Fatalf("pending playback was not consumed: %#v", model.pendingPlayback)
	}
}

func TestUserQuitDoesNotResume(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5", VideoURL: "https://example.test/video.m3u8",
	}
	updated, command := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 61, Duration: 100, Reason: "quit"},
	})
	model = updated.(Model)
	if command != nil {
		t.Fatal("user quit unexpectedly resumed playback")
	}
	if !strings.Contains(model.status, "Playback stopped at") {
		t.Fatalf("status = %q, want stopped message", model.status)
	}
}

func TestClosingResumedPlaybackReturnsHome(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.stage = stageStreams
	model.playbackReturnHome = true
	model.details.Episodes = []catalog.Episode{{URL: "episode-5", EpisodeNumber: "5"}}
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5",
		VideoURL: "http://localhost:50000/m3u8?url=fresh",
	}

	updated, _ := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 61, Duration: 100, Reason: "quit"},
	})
	model = updated.(Model)

	if model.stage != stageSources {
		t.Fatalf("stage = %d, want home (%d)", model.stage, stageSources)
	}
	if model.homeFocus != homeFocusResumeWatching {
		t.Fatalf("home focus = %d, want Resume Watching", model.homeFocus)
	}
	if model.playbackReturnHome {
		t.Fatal("playback return-home flag was not cleared")
	}
}

func TestClosingEpisodePlaybackReturnsEpisodeSelector(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.stage = stageStreams
	model.details.Episodes = []catalog.Episode{{URL: "episode-5", EpisodeNumber: "5"}}
	model.setEpisodeItems()
	playback := appconfig.ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5",
		VideoURL: "http://localhost:50000/m3u8?url=fresh",
	}

	updated, _ := model.Update(playerFinishedMsg{
		entry:    &playback,
		progress: &player.Progress{Position: 61, Duration: 100, Reason: "quit"},
	})
	model = updated.(Model)

	if model.stage != stageEpisodes {
		t.Fatalf("stage = %d, want episode selector (%d)", model.stage, stageEpisodes)
	}
}

func TestIsEarlyPlaybackStop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		progress player.Progress
		want     bool
	}{
		{name: "early eof", progress: player.Progress{Position: 61, Duration: 100, Reason: "eof"}, want: true},
		{name: "complete eof", progress: player.Progress{Position: 95, Duration: 100, Reason: "eof"}},
		{name: "user quit", progress: player.Progress{Position: 61, Duration: 100, Reason: "quit"}},
		{name: "short start", progress: player.Progress{Position: 2, Duration: 100, Reason: "eof"}},
		{name: "unknown reason", progress: player.Progress{Position: 61, Duration: 100}},
		{name: "no duration", progress: player.Progress{Position: 61, Reason: "eof"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isEarlyPlaybackStop(test.progress); got != test.want {
				t.Fatalf("isEarlyPlaybackStop(%#v) = %t, want %t", test.progress, got, test.want)
			}
		})
	}
}

func TestPlaySelectedKeepsSavedEpisodePosition(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.width, model.height = 100, 40
	model.selectedSource = catalog.Source{ID: "source", Name: "Source", Type: "anime"}
	model.selectedMedia = catalog.Media{URL: "media", Title: "Media"}
	model.selectedEpisode = catalog.Episode{URL: "episode-5", Name: "Episode 5"}
	model.videos = []catalog.Video{{URL: "https://example.test/video.m3u8", Quality: "1080p"}}
	model.continueWatching = []appconfig.ContinueEntry{{
		SourceID: "source", MediaURL: "media", EpisodeURL: "episode-5", EpisodeNumber: "5",
		Position: 872.45, Duration: 1419.96, VideoURL: "https://example.test/old.m3u8",
	}}

	updated, _ := model.playSelected()
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("playSelected: %v", model.err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(config.ContinueWatching) != 1 {
		t.Fatalf("continue watching = %#v", config.ContinueWatching)
	}
	saved := config.ContinueWatching[0]
	if saved.Position != 872.45 || saved.Duration != 1419.96 {
		t.Fatalf("saved position = %v/%v, want 872.45/1419.96", saved.Position, saved.Duration)
	}
	if saved.VideoURL != "https://example.test/video.m3u8" {
		t.Fatalf("saved video url = %q, want freshly resolved url", saved.VideoURL)
	}
}

func TestPlaySelectedRestartsFinishedEpisode(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.width, model.height = 100, 40
	model.selectedSource = catalog.Source{ID: "source", Name: "Source", Type: "anime"}
	model.selectedMedia = catalog.Media{URL: "media", Title: "Media"}
	model.selectedEpisode = catalog.Episode{URL: "episode-5", Name: "Episode 5"}
	model.videos = []catalog.Video{{URL: "https://example.test/video.m3u8", Quality: "1080p"}}
	model.continueWatching = []appconfig.ContinueEntry{{
		SourceID: "source", MediaURL: "media", EpisodeURL: "episode-5", EpisodeNumber: "5",
		Position: 1419.96, Duration: 1419.96, VideoURL: "https://example.test/old.m3u8",
	}}

	updated, _ := model.playSelected()
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("playSelected: %v", model.err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if position := config.ContinueWatching[0].Position; position != 0 {
		t.Fatalf("saved position = %v, want 0 for a finished episode", position)
	}
}

func TestExistingCompletedPlaybackCountsAsLocallyWatched(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.continueWatching = []appconfig.ContinueEntry{{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "5", Position: 91, Duration: 100,
	}}
	if watched := model.watchedEpisodesFor("source", "media"); len(watched) != 1 || watched[0] != "5" {
		t.Fatalf("inferred locally watched episodes = %#v", watched)
	}
}

func TestTrackerWatchedEpisodeRemainsSelectable(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.width, model.height = 100, 40
	model.loading = false
	model.stage = stageEpisodes
	model.selectedSource = catalog.Source{Type: "anime"}
	tracked := anilist.Entry{Progress: 1}
	model.resolvingEpisode = &tracked
	model.details = catalog.Media{Episodes: []catalog.Episode{
		{Name: "Episode 2", EpisodeNumber: "2"},
		{Name: "Episode 1", EpisodeNumber: "1"},
	}}
	model.setEpisodeItems()
	model.episodeList.Select(0)

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(Model)
	if model.episodeList.Index() != 1 {
		t.Fatalf("watched episode index = %d, want 1", model.episodeList.Index())
	}
	updated, command := model.activate()
	model = updated.(Model)
	if command == nil || model.selectedEpisode.Number() != "1" {
		t.Fatalf("selected watched episode = %#v, command nil = %v", model.selectedEpisode, command == nil)
	}
}

func TestEpisodeSubtitleUsesMetadataThenFallbacks(t *testing.T) {
	t.Parallel()
	if got := episodeSubtitle(catalog.Episode{Title: "The Battle", Name: "Episode 10", EpisodeNumber: "10"}, "Bleach"); got != "The Battle" {
		t.Fatalf("metadata subtitle = %q", got)
	}
	if got := episodeSubtitle(catalog.Episode{Name: "Episode 10 - The Battle", EpisodeNumber: "10"}, "Bleach"); got != "The Battle" {
		t.Fatalf("provider subtitle = %q", got)
	}
	if got := episodeSubtitle(catalog.Episode{Name: "Episode 10", EpisodeNumber: "10"}, "Bleach"); got != "Bleach" {
		t.Fatalf("series fallback subtitle = %q", got)
	}
}

func TestSavedStreamPreferenceAutoPlaysForSameMedia(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	tracked := anilist.Entry{MediaID: 42, Title: "Series"}
	model.resolvingEpisode = &tracked
	model.streamPreferences = []appconfig.StreamPreference{{MediaID: 42, Value: "SubsPlease (Dub) - 1080p"}}

	updated, command := model.Update(videosMsg{items: []catalog.Video{
		{Quality: "SubsPlease (Sub) - 1080p", URL: "sub"},
		{Quality: "SubsPlease (Dub) - 1080p", URL: "dub"},
		{Quality: "Other - 720p", URL: "other"},
	}})
	model = updated.(Model)
	if command == nil || model.stage != stageStreams || model.cursor != 1 {
		t.Fatalf("auto-play state = stage %d, cursor %d, command nil %v", model.stage, model.cursor, command == nil)
	}
	if !model.rememberStream {
		t.Fatal("remember checkbox did not default on for a saved preference")
	}
}

func TestSingleStreamAutoPlaysWithoutPreference(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.resolvingEpisode = &anilist.Entry{MediaID: 42, Title: "Series"}

	updated, command := model.Update(videosMsg{items: []catalog.Video{{Quality: "Only", URL: "only"}}})
	model = updated.(Model)
	if command == nil || model.stage != stageStreams || model.cursor != 0 {
		t.Fatalf("single stream state = stage %d, command nil %v", model.stage, command == nil)
	}
	if model.rememberStream {
		t.Fatal("remember checkbox defaulted on without a saved preference")
	}
}

func TestRememberServerSavesAndClearingRemovesPreference(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.json"))
	model := New(nil, store, "", "mpv")
	model.loading = false
	model.stage = stageStreams
	model.resolvingEpisode = &anilist.Entry{MediaID: 42, Title: "Series"}
	model.videos = []catalog.Video{{Quality: "A - 1080p", URL: "a"}}
	model.rememberStream = true

	model.rememberSelectedStream()
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.StreamPreferences) != 1 || config.StreamPreferences[0].MediaID != 42 || config.StreamPreferences[0].Value != "A - 1080p" {
		t.Fatalf("saved preference = %#v", config.StreamPreferences)
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	model = updated.(Model)
	if model.rememberStream {
		t.Fatal("remember checkbox stayed on")
	}
	config, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.StreamPreferences) != 0 {
		t.Fatalf("preference not cleared: %#v", config.StreamPreferences)
	}
}

func TestDubPreferenceSkipsSubbedStream(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.resolvingEpisode = &anilist.Entry{MediaID: 42}
	model.streamPreferences = []appconfig.StreamPreference{{MediaID: 42, Value: "1080p (Dub)"}}

	index, ok := model.automaticVideoIndex([]catalog.Video{
		{Quality: "1080p", URL: "sub"},
		{Quality: "1080p (Dub)", URL: "dub"},
	})
	if !ok || index != 1 {
		t.Fatalf("dub-mismatched auto index = %d, ok %v", index, ok)
	}
}

func TestResumeWatchingFiltersCompletedPlayback(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.continueWatching = []appconfig.ContinueEntry{
		{Title: "Unknown progress"},
		{Title: "Still watching", Position: 98.9, Duration: 100},
		{Title: "Completed", Position: 99, Duration: 100},
		{Title: "Past end", Position: 101, Duration: 100},
	}
	model.refreshResumeWatching()
	if len(model.resumeWatchingList.Items()) != 2 {
		t.Fatalf("Resume Watching items = %d", len(model.resumeWatchingList.Items()))
	}
	first := model.resumeWatchingList.Items()[0].(resumeWatchingListItem)
	second := model.resumeWatchingList.Items()[1].(resumeWatchingListItem)
	if first.entry.Title != "Unknown progress" || second.entry.Title != "Still watching" {
		t.Fatalf("Resume Watching order = %q, %q", first.entry.Title, second.entry.Title)
	}
}

func TestCloudflareErrorDetection(t *testing.T) {
	t.Parallel()
	for _, message := range []string{"HTTP 403 Forbidden", "status 429", "too many requests", "status 503", "Cloudflare challenge", "missing cf_clearance", "captcha required"} {
		if !isCloudflareError(fmt.Errorf("%s", message)) {
			t.Errorf("did not detect %q", message)
		}
	}
	if isCloudflareError(fmt.Errorf("connection refused")) {
		t.Fatal("ordinary network error detected as Cloudflare")
	}
}

func TestCloudflareCookieCanBeCleared(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.stage = stageCloudflareCookie
	model.loading = false
	model.cookieInput.SetValue("cf_clearance=stale")
	model.cookieInput.Focus()

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = updated.(Model)
	if model.cookieInput.Value() != "" {
		t.Fatalf("cookie after ctrl+u = %q, want empty", model.cookieInput.Value())
	}
}

func TestExtensionSearchFiltersListWhileTyping(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.loading = false
	model.stage = stageExtensionSearch
	model.availableExtensions = []repository.Extension{
		{Name: "AnimePahe", Package: "animepahe", Language: "en"},
		{Name: "AllAnime", Package: "allanime", Language: "en"},
	}
	model.extensionSearch.Focus()

	for _, character := range "pahe" {
		updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		model = updated.(Model)
	}
	filtered := model.filteredExtensions()
	if len(filtered) != 1 || filtered[0].Name != "AnimePahe" {
		t.Fatalf("live filtered extensions = %#v", filtered)
	}
	if !strings.Contains(model.View(), "AnimePahe") || strings.Contains(model.View(), "AllAnime") {
		t.Fatal("search view did not render the live filtered list")
	}
}

func TestInstalledExtensionCanBeSelectedForRemoval(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	extension := repository.Extension{Name: "AnimePahe", Package: "animepahe", Language: "en", Version: "1"}
	if err := os.WriteFile(filepath.Join(directory, "animepahe.jar"), []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := New(nil, nil, directory, "mpv")
	model.loading = false
	model.stage = stageAvailableExtensions
	model.width = 100
	model.availableExtensions = []repository.Extension{extension}

	if view := model.View(); !strings.Contains(view, "[installed]") || !strings.Contains(view, "d uninstall") {
		t.Fatalf("installed extension actions missing:\n%s", view)
	}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = updated.(Model)
	if command == nil || !model.loading {
		t.Fatalf("uninstall was not started: %#v", model)
	}
}

func TestLoadingSpinnerDecoratesSelectedResultWithoutAddingStatusRows(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.loading = true
	model.stage = stageResults
	model.selectedSource = catalog.Source{Name: "AnimePahe"}
	model.results = []catalog.Media{{Title: "One Piece"}}

	idle := model
	idle.loading = false
	idleRow := ansi.Strip(idle.listView([]string{"One Piece"}))
	loadingRow := ansi.Strip(model.listView([]string{"One Piece"}))
	view := model.View()
	if strings.Contains(view, "Working with the AnymeX") {
		t.Fatalf("loading view added a top status row:\n%s", view)
	}
	if !strings.Contains(loadingRow, ansi.Strip(model.spinner.View())+" One Piece") {
		t.Fatalf("selected result does not contain spinner:\n%s", view)
	}
	if titleColumn(idleRow, "One Piece") != titleColumn(loadingRow, "One Piece") {
		t.Fatalf("selected result shifted while loading: idle=%q loading=%q", idleRow, loadingRow)
	}
}

func TestLoadingSpinnerDecoratesSelectedHomeItem(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.width, model.height = 100, 40
	model.loading = false
	model.stage = stageSources
	model.homeType = "anime"
	model.homeFocus = homeFocusNewEpisodes
	model.newEpisodesList.SetSize(92, 3)
	_ = model.newEpisodesList.SetItems([]list.Item{newEpisodeListItem{entry: anilist.Entry{Title: "Overgeared"}}})

	idleView := ansi.Strip(model.View())
	model.loading = true
	startupView := ansi.Strip(model.View())
	if strings.Contains(startupView, ansi.Strip(model.spinner.View())) {
		t.Fatalf("startup loading decorated the selected Home item:\n%s", startupView)
	}
	resolving := anilist.Entry{Title: "Overgeared"}
	model.resolvingEpisode = &resolving
	view := model.View()
	loadingView := ansi.Strip(view)
	if !strings.Contains(loadingView, ansi.Strip(model.spinner.View())) || !strings.Contains(view, "Overgeared") {
		t.Fatalf("selected Home item does not contain spinner:\n%s", view)
	}
	if titleColumn(idleView, "Overgeared") != titleColumn(loadingView, "Overgeared") {
		t.Fatalf("selected Home title shifted while loading:\nidle:\n%s\nloading:\n%s", idleView, loadingView)
	}
	if strings.Contains(loadingView, "[0m") {
		t.Fatalf("spinner leaked an ANSI reset sequence:\n%s", loadingView)
	}
	if strings.Contains(view, "Working with the AnymeX") {
		t.Fatalf("loading view added a top status row:\n%s", view)
	}
}

func TestRefreshingResumeDecoratesSelectedRowWithoutShiftingHome(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.width, model.height = 100, 40
	model.stage = stageSources
	model.homeType = "anime"
	model.homeFocus = homeFocusResumeWatching
	model.resumeWatchingList.SetSize(92, 3)
	entry := appconfig.ContinueEntry{Title: "Overgeared", EpisodeNumber: "5", Position: 879, Duration: 1420}
	_ = model.resumeWatchingList.SetItems([]list.Item{resumeWatchingListItem{entry: entry}})

	idleView := ansi.Strip(model.View())
	model.loading = true
	model.pendingPlayback = &entry
	loadingView := ansi.Strip(model.View())

	if !strings.Contains(loadingView, ansi.Strip(model.spinner.View())) || !strings.Contains(loadingView, "Overgeared") {
		t.Fatalf("refreshing Resume Watching row has no spinner:\n%s", loadingView)
	}
	if titleColumn(idleView, "Overgeared") != titleColumn(loadingView, "Overgeared") {
		t.Fatalf("Resume Watching title shifted while refreshing:\nidle:\n%s\nloading:\n%s", idleView, loadingView)
	}
	if len(strings.Split(idleView, "\n")) != len(strings.Split(loadingView, "\n")) {
		t.Fatalf("Home row count changed while refreshing:\nidle:\n%s\nloading:\n%s", idleView, loadingView)
	}
	if strings.Contains(loadingView, "Refreshing stream") {
		t.Fatalf("refresh status leaked above Home:\n%s", loadingView)
	}
}

func titleColumn(view, title string) int {
	for _, line := range strings.Split(view, "\n") {
		if index := strings.Index(line, title); index >= 0 {
			return ansi.StringWidth(line[:index])
		}
	}
	return -1
}

func TestCloudflareRecoveryBreadcrumbUsesReturnStage(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.stage = stageCloudflareCookie
	model.cloudflareReturn = stageResults
	model.selectedSource = catalog.Source{Name: "AnimePahe"}

	if got, want := model.breadcrumb(), "Home › AnimePahe › Results"; got != want {
		t.Fatalf("breadcrumb = %q, want %q", got, want)
	}
}

func TestExtensionRequestsDoNotAppearStuck(t *testing.T) {
	t.Parallel()
	if extensionRequestTimeout > 10*time.Second {
		t.Fatalf("extension request timeout = %s, want at most 10s", extensionRequestTimeout)
	}
}

func TestCalculateReleaseDateUsesPreviousWeeklyAiring(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	entry := anilist.Entry{NextAiringEpisode: &anilist.NextAiringEpisode{
		Episode:  7,
		AiringAt: now.Add(3 * 24 * time.Hour).Unix(),
	}}
	got := calculateReleaseDate(entry, 6, now)
	want := now.Add(-4 * 24 * time.Hour)
	if got == nil || !got.Equal(want) {
		t.Fatalf("release date = %v, want %v", got, want)
	}
}

func TestSkipTimesBecomeMpvChapterMarkers(t *testing.T) {
	t.Parallel()
	chapters := skipTimesChapters(aniskip.Times{
		Op:    &aniskip.Interval{Start: 47.5, End: 137.5},
		Ed:    &aniskip.Interval{Start: 1342, End: 1430},
		Recap: &aniskip.Interval{Start: 5, End: 20},
	})
	want := []player.Chapter{
		{Start: 5, End: 20, Title: "Recap"},
		{Start: 20, End: 20, Title: "Recap end"},
		{Start: 47.5, End: 137.5, Title: "Intro"},
		{Start: 137.5, End: 137.5, Title: "Intro end"},
		{Start: 1342, End: 1430, Title: "Outro"},
		{Start: 1430, End: 1430, Title: "Outro end"},
	}
	if len(chapters) != len(want) {
		t.Fatalf("chapters = %#v", chapters)
	}
	for index := range want {
		if chapters[index] != want[index] {
			t.Fatalf("chapter %d = %#v, want %#v", index, chapters[index], want[index])
		}
	}
}
