package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Repository struct {
	URL  string `json:"url" toml:"url"`
	Type string `json:"type" toml:"type"`
}

type Config struct {
	Repositories      []Repository       `json:"repositories" toml:"repositories"`
	AniListToken      string             `json:"anilistToken,omitempty" toml:"anilist_token,omitempty"`
	ContinueWatching  []ContinueEntry    `json:"continueWatching,omitempty" toml:"continue_watching,omitempty"`
	ActiveSources     map[string]string  `json:"activeSources,omitempty" toml:"active_sources,omitempty"`
	MediaMappings     []MediaMapping     `json:"mediaMappings,omitempty" toml:"media_mappings,omitempty"`
	SourceCredentials []SourceCredential `json:"sourceCredentials,omitempty" toml:"source_credentials,omitempty"`
	StreamPreferences []StreamPreference `json:"streamPreferences,omitempty" toml:"stream_preferences,omitempty"`
	CheckUpdates      *bool              `json:"checkUpdates,omitempty" toml:"check_updates,omitempty"`
}

func (c Config) UpdatesEnabled() bool { return c.CheckUpdates == nil || *c.CheckUpdates }

type SourceCredential struct {
	SourceID  string `json:"sourceId" toml:"source_id"`
	URL       string `json:"url" toml:"url"`
	Cookie    string `json:"cookie" toml:"cookie"`
	UserAgent string `json:"userAgent" toml:"user_agent"`
}

type MediaMapping struct {
	MediaID  int    `json:"mediaId" toml:"media_id"`
	SourceID string `json:"sourceId" toml:"source_id"`
	Title    string `json:"title" toml:"title"`
}

type StreamPreference struct {
	MediaID int    `json:"mediaId" toml:"media_id"`
	Value   string `json:"value" toml:"value"`
}

type ContinueEntry struct {
	Title              string            `json:"title" toml:"title"`
	SourceID           string            `json:"sourceId" toml:"source_id"`
	SourceName         string            `json:"sourceName" toml:"source_name"`
	MediaURL           string            `json:"mediaUrl" toml:"media_url"`
	EpisodeURL         string            `json:"episodeUrl" toml:"episode_url"`
	EpisodeName        string            `json:"episodeName" toml:"episode_name"`
	EpisodeNumber      string            `json:"episodeNumber" toml:"episode_number"`
	VideoURL           string            `json:"videoUrl" toml:"video_url"`
	Quality            string            `json:"quality" toml:"quality"`
	Headers            map[string]string `json:"headers,omitempty" toml:"headers,omitempty"`
	Subtitles          []string          `json:"subtitles,omitempty" toml:"subtitles,omitempty"`
	UpdatedAt          int64             `json:"updatedAt" toml:"updated_at"`
	Position           float64           `json:"position,omitempty" toml:"position,omitempty"`
	Duration           float64           `json:"duration,omitempty" toml:"duration,omitempty"`
	WatchedEpisodes    []string          `json:"watchedEpisodes,omitempty" toml:"watched_episodes,omitempty"`
	AniListEntryID     int               `json:"aniListEntryId,omitempty" toml:"anilist_entry_id,omitempty"`
	AniListMediaID     int               `json:"aniListMediaId,omitempty" toml:"anilist_media_id,omitempty"`
	AniListProgress    int               `json:"aniListProgress,omitempty" toml:"anilist_progress,omitempty"`
	AniListTotal       int               `json:"aniListTotal,omitempty" toml:"anilist_total,omitempty"`
	AniListStatus      string            `json:"aniListStatus,omitempty" toml:"anilist_status,omitempty"`
	AniListMediaStatus string            `json:"aniListMediaStatus,omitempty" toml:"anilist_media_status,omitempty"`
}

func (s *Store) SetAniListToken(token string) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	config.AniListToken = token
	return s.Save(config)
}

func (s *Store) SaveContinue(entry ContinueEntry) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := make([]ContinueEntry, 0, len(config.ContinueWatching)+1)
	for _, existing := range config.ContinueWatching {
		if existing.SourceID == entry.SourceID && existing.MediaURL == entry.MediaURL {
			entry.WatchedEpisodes = mergeStrings(existing.WatchedEpisodes, entry.WatchedEpisodes)
			continue
		}
		filtered = append(filtered, existing)
		if len(filtered) == 9 {
			break
		}
	}
	filtered = append([]ContinueEntry{entry}, filtered...)
	config.ContinueWatching = filtered
	return s.Save(config)
}

func mergeStrings(existing, added []string) []string {
	seen := make(map[string]bool, len(existing)+len(added))
	result := make([]string, 0, len(existing)+len(added))
	for _, values := range [][]string{existing, added} {
		for _, value := range values {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (s *Store) SetActiveSource(itemType, sourceID string) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	if config.ActiveSources == nil {
		config.ActiveSources = make(map[string]string)
	}
	config.ActiveSources[itemType] = sourceID
	return s.Save(config)
}

func (s *Store) SaveMediaMapping(mapping MediaMapping) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := make([]MediaMapping, 0, len(config.MediaMappings)+1)
	filtered = append(filtered, mapping)
	for _, existing := range config.MediaMappings {
		if existing.MediaID == mapping.MediaID && existing.SourceID == mapping.SourceID {
			continue
		}
		filtered = append(filtered, existing)
	}
	config.MediaMappings = filtered
	return s.Save(config)
}

func (s *Store) SaveSourceCredential(credential SourceCredential) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := []SourceCredential{credential}
	for _, existing := range config.SourceCredentials {
		if existing.SourceID != credential.SourceID {
			filtered = append(filtered, existing)
		}
	}
	config.SourceCredentials = filtered
	return s.Save(config)
}

func (s *Store) SaveStreamPreference(preference StreamPreference) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := make([]StreamPreference, 0, len(config.StreamPreferences)+1)
	if preference.MediaID != 0 && preference.Value != "" {
		filtered = append(filtered, preference)
	}
	for _, existing := range config.StreamPreferences {
		if existing.MediaID != preference.MediaID {
			filtered = append(filtered, existing)
		}
	}
	config.StreamPreferences = filtered
	return s.Save(config)
}

func (s *Store) ClearStreamPreference(mediaID int) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := make([]StreamPreference, 0, len(config.StreamPreferences))
	for _, existing := range config.StreamPreferences {
		if existing.MediaID != mediaID {
			filtered = append(filtered, existing)
		}
	}
	config.StreamPreferences = filtered
	return s.Save(config)
}

type Store struct {
	path string
}

func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "anymex-cli", "config.toml"), nil
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.migrateLegacyJSON()
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if _, err := toml.Decode(string(data), &config); err == nil {
		return config, nil
	}
	// Custom --config paths may still point at a legacy JSON file. Read them so
	// users can open AnimiX and save the next change as TOML.
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", s.path, err)
	}
	return config, nil
}

func (s *Store) Save(config Config) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encodeErr := toml.NewEncoder(file).Encode(config)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func (s *Store) Ensure() error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		enabled := true
		config.CheckUpdates = &enabled
		return s.Save(config)
	}
	return err
}

func (s *Store) migrateLegacyJSON() (Config, error) {
	if !strings.EqualFold(filepath.Ext(s.path), ".toml") {
		return Config{}, nil
	}
	legacy := strings.TrimSuffix(s.path, filepath.Ext(s.path)) + ".json"
	data, err := os.ReadFile(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode legacy config %s: %w", legacy, err)
	}
	if err := s.Save(config); err != nil {
		return Config{}, fmt.Errorf("write migrated TOML config: %w", err)
	}
	if err := os.Rename(legacy, legacy+".bak"); err != nil {
		return Config{}, fmt.Errorf("back up legacy config: %w", err)
	}
	return config, nil
}

func (s *Store) Add(repository Repository) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	for _, existing := range config.Repositories {
		if existing.URL == repository.URL && existing.Type == repository.Type {
			return nil
		}
	}
	config.Repositories = append(config.Repositories, repository)
	return s.Save(config)
}

func (s *Store) Remove(repository Repository) error {
	config, err := s.Load()
	if err != nil {
		return err
	}
	filtered := config.Repositories[:0]
	for _, existing := range config.Repositories {
		if existing.URL != repository.URL || existing.Type != repository.Type {
			filtered = append(filtered, existing)
		}
	}
	config.Repositories = filtered
	return s.Save(config)
}
