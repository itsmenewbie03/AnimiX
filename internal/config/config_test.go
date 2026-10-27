package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestStoreAddAndRemove(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
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
	path := filepath.Join(t.TempDir(), "config.json")
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
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
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
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
	entry := ContinueEntry{
		SourceID: "source", MediaURL: "media", EpisodeNumber: "4",
		AniListEntryID: 10, AniListMediaID: 99, AniListProgress: 3,
		AniListTotal: 12, AniListStatus: "CURRENT", AniListMediaStatus: "RELEASING",
	}
	if err := store.SaveContinue(entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ContinueWatching) != 1 || loaded.ContinueWatching[0].AniListEntryID != 10 || loaded.ContinueWatching[0].AniListMediaID != 99 {
		t.Fatalf("AniList playback link = %#v", loaded.ContinueWatching)
	}
}

func TestSaveContinueMergesLocallyWatchedEpisodes(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
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
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
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
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
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
