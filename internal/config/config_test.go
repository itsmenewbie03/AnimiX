package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultUpdateChecksEnabledUnlessDisabled(t *testing.T) {
	t.Parallel()
	if !(Config{}).UpdatesEnabled() {
		t.Fatal("updates should be enabled by default")
	}
	disabled := false
	if (Config{CheckUpdates: &disabled}).UpdatesEnabled() {
		t.Fatal("updates should respect check_updates = false")
	}
}

func TestResumeAniListSyncDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	store := NewStore(path)

	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.ResumeAniListSyncEnabled() {
		t.Fatal("Resume AniList sync should be enabled by default")
	}

	disabled := false
	config.SyncResumeWithAniList = &disabled
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResumeAniListSyncEnabled() {
		t.Fatal("sync_resume_with_anilist = false was ignored")
	}
	settingsData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settingsData), "sync_resume_with_anilist = false") {
		t.Fatalf("config.toml missing Resume sync setting: %s", settingsData)
	}
}

func TestAutoPlayNextEpisodeUsesUnsetAskState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	store := NewStore(path)

	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.AutoPlayNextEpisode != nil {
		t.Fatalf("default auto-play setting = %v, want unset", *config.AutoPlayNextEpisode)
	}

	for _, enabled := range []bool{true, false} {
		if err := store.SetAutoPlayNextEpisode(enabled); err != nil {
			t.Fatal(err)
		}
		config, err = store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if config.AutoPlayNextEpisode == nil || *config.AutoPlayNextEpisode != enabled {
			t.Fatalf("saved auto-play setting = %v, want %t", config.AutoPlayNextEpisode, enabled)
		}
	}

	settingsData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settingsData), "auto_play_next_episode = false") {
		t.Fatalf("config.toml missing auto-play setting: %s", settingsData)
	}
	stateData, err := os.ReadFile(filepath.Join(filepath.Dir(path), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateData), "autoPlayNextEpisode") {
		t.Fatalf("state.json contains user setting: %s", stateData)
	}
}

func TestLoadMigratesLegacyJSONAndKeepsBackup(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	legacy := filepath.Join(directory, "config.json")
	path := filepath.Join(directory, "config.toml")
	want := Config{AniListToken: "secret", Repositories: []Repository{{URL: "https://example.test", Type: "anime"}}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AniListToken != want.AniListToken || len(got.Repositories) != 1 {
		t.Fatalf("migrated config = %#v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("TOML config missing: %v", err)
	}
	settingsData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(settingsData) != "" {
		t.Fatalf("config.toml contains application state: %s", settingsData)
	}
	stateData, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
	if !strings.Contains(string(stateData), `"anilistToken": "secret"`) {
		t.Fatalf("state.json does not contain migrated state: %s", stateData)
	}
	if _, err := os.Stat(legacy + ".bak"); err != nil {
		t.Fatalf("JSON backup missing: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy JSON still active: %v", err)
	}
}

func TestLoadSplitsCombinedTOMLAndKeepsBackup(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	combined := "check_updates = false\nanilist_token = \"secret\"\n\n[[repositories]]\nurl = \"https://example.test\"\ntype = \"anime\"\n"
	if err := os.WriteFile(path, []byte(combined), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpdatesEnabled() || loaded.AniListToken != "secret" || len(loaded.Repositories) != 1 {
		t.Fatalf("split config = %#v", loaded)
	}
	settingsData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settingsData), "anilist_token") || strings.Contains(string(settingsData), "repositories") {
		t.Fatalf("config.toml still contains state: %s", settingsData)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("combined TOML backup missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "state.json")); err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
}

func TestEnsureCreatesEditableTOML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := NewStore(path).Ensure(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("empty config file")
	}
}

func TestStoreAddAndRemove(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	repo := Repository{URL: "https://example.org/index.min.json", Type: "anime"}
	if err := store.Add(repo); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(repo); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Repositories) != 1 || config.Repositories[0] != repo {
		t.Fatalf("repositories = %#v", config.Repositories)
	}
	if err := store.SetAniListToken("secret"); err != nil {
		t.Fatal(err)
	}
	config, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.AniListToken != "secret" || len(config.Repositories) != 1 {
		t.Fatalf("config after token = %#v", config)
	}
	if err := store.Remove(repo); err != nil {
		t.Fatal(err)
	}
	config, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Repositories) != 0 {
		t.Fatalf("repositories after remove = %#v", config.Repositories)
	}
}

func TestSaveSourceCredentialReplacesSourceAndUsesPrivateFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	store := NewStore(path)
	if err := store.SaveSourceCredential(SourceCredential{SourceID: "source-a", URL: "https://example.test", Cookie: "old", UserAgent: "ua"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSourceCredential(SourceCredential{SourceID: "source-a", URL: "https://example.test", Cookie: "new", UserAgent: "ua"}); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.SourceCredentials) != 1 || config.SourceCredentials[0].Cookie != "new" {
		t.Fatalf("source credentials = %#v", config.SourceCredentials)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o", info.Mode().Perm())
	}
}

func TestSaveContinueKeepsNewestTenAndReplacesMedia(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	for index := 0; index < 11; index++ {
		entry := ContinueEntry{SourceID: "source", MediaURL: "media-" + strconv.Itoa(index), UpdatedAt: int64(index)}
		if err := store.SaveContinue(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveContinue(ContinueEntry{SourceID: "source", MediaURL: "media-5", EpisodeNumber: "8", UpdatedAt: 99}); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.ContinueWatching) != 10 {
		t.Fatalf("ContinueWatching length = %d", len(config.ContinueWatching))
	}
	first := config.ContinueWatching[0]
	if first.MediaURL != "media-5" || first.EpisodeNumber != "8" || first.UpdatedAt != 99 {
		t.Fatalf("newest entry = %#v", first)
	}
	duplicates := 0
	for _, entry := range config.ContinueWatching {
		if entry.MediaURL == "media-5" {
			duplicates++
		}
	}
	if duplicates != 1 {
		t.Fatalf("media-5 entries = %d", duplicates)
	}
}

func TestSaveContinuePersistsAniListLink(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	entry := ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "4",
		AniListEntryID: 10, AniListMediaID: 99, AniListProgress: 3,
		MALID: 12345, AniListTotal: 12, AniListStatus: "CURRENT", AniListMediaStatus: "RELEASING",
	}
	if err := store.SaveContinue(entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ContinueWatching) != 1 || loaded.ContinueWatching[0].AniListEntryID != 10 || loaded.ContinueWatching[0].AniListMediaID != 99 || loaded.ContinueWatching[0].MALID != 12345 {
		t.Fatalf("AniList playback link = %#v", loaded.ContinueWatching)
	}
}

func TestSaveContinueMergesLocallyWatchedEpisodes(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	for _, watched := range [][]string{{"1", "2"}, {"2", "3"}} {
		if err := store.SaveContinue(ContinueEntry{SourceID: "source", MediaURL: "media", WatchedEpisodes: watched}); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.ContinueWatching[0].WatchedEpisodes; len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Fatalf("watched episodes = %#v", got)
	}
}

func TestSaveStreamPreferenceReplacesSameMedia(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	if err := store.SaveStreamPreference(StreamPreference{MediaID: 42, Value: "720p"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStreamPreference(StreamPreference{MediaID: 42, Value: "1080p"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStreamPreference(StreamPreference{MediaID: 7, Value: "480p"}); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.StreamPreferences) != 2 || config.StreamPreferences[0].MediaID != 7 || config.StreamPreferences[1].Value != "1080p" {
		t.Fatalf("stream preferences = %#v", config.StreamPreferences)
	}
	if err := store.ClearStreamPreference(42); err != nil {
		t.Fatal(err)
	}
	config, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.StreamPreferences) != 1 || config.StreamPreferences[0].MediaID != 7 {
		t.Fatalf("stream preferences after clear = %#v", config.StreamPreferences)
	}
}

func TestStorePersistsActiveSourceAndMediaMapping(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	if err := store.SetActiveSource("anime", "source-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMediaMapping(MediaMapping{MediaID: 42, SourceID: "source-a", Title: "First title"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMediaMapping(MediaMapping{MediaID: 42, SourceID: "source-a", Title: "Correct title"}); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.ActiveSources["anime"] != "source-a" {
		t.Fatalf("active source = %q", config.ActiveSources["anime"])
	}
	if len(config.MediaMappings) != 1 || config.MediaMappings[0].Title != "Correct title" {
		t.Fatalf("media mappings = %#v", config.MediaMappings)
	}
}
