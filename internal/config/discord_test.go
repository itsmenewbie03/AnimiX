package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscordSettingsAndCredentialStaySeparateAcrossHistorySaves(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "config.toml"))
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.DiscordPreferences().Enabled || config.DiscordPreferences().ShowBrowsing {
		t.Fatal("Discord sharing must default off")
	}
	config.DiscordToken = "discord-fixture-token"
	config.Discord = &DiscordSettings{ApplicationID: "123", ShowArtwork: true}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContinue(ContinueEntry{Title: "Anime", CoverURL: "https://images.example/anime.jpg"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMangaReading(MangaReadingEntry{Title: "Manga", CoverURL: "https://images.example/manga.jpg"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DiscordToken != config.DiscordToken || loaded.DiscordPreferences() != *config.Discord || loaded.DiscordPreferences().Enabled {
		t.Fatalf("Discord settings changed while saving history: %#v", loaded.Discord)
	}
	settings, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), config.DiscordToken) || !strings.Contains(string(settings), "[discord]") {
		t.Fatal("settings file lost preferences or exposed credential")
	}
	state, err := os.ReadFile(store.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), config.DiscordToken) || strings.Contains(string(state), "showArtwork") {
		t.Fatal("managed state lost credential or contains preferences")
	}
	loaded.DiscordToken = ""
	loaded.Discord.Enabled = false
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	state, _ = os.ReadFile(store.statePath)
	if strings.Contains(string(state), config.DiscordToken) {
		t.Fatal("forgetting token left credential in managed state")
	}
}

func TestDiscordCombinedTOMLMigrationPreservesExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := "discord_token = \"fixture\"\n[discord]\nenabled = false\nshow_browsing = false\napplication_id = \"123\"\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DiscordToken != "fixture" || loaded.DiscordPreferences().Enabled || loaded.DiscordPreferences().ApplicationID != "123" {
		t.Fatalf("migrated preferences = %#v", loaded.Discord)
	}
	settings, _ := os.ReadFile(path)
	if strings.Contains(string(settings), "discord_token") {
		t.Fatal("migration kept credential in editable TOML")
	}
}
