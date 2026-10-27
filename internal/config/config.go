package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Repository struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type Config struct {
	Repositories      []Repository       `json:"repositories"`
	AniListToken      string             `json:"anilistToken,omitempty"`
	ContinueWatching  []ContinueEntry    `json:"continueWatching,omitempty"`
	ActiveSources     map[string]string  `json:"activeSources,omitempty"`
	MediaMappings     []MediaMapping     `json:"mediaMappings,omitempty"`
	SourceCredentials []SourceCredential `json:"sourceCredentials,omitempty"`
	StreamPreferences []StreamPreference `json:"streamPreferences,omitempty"`
}

type SourceCredential struct {
	SourceID  string `json:"sourceId"`
	URL       string `json:"url"`
	Cookie    string `json:"cookie"`
	UserAgent string `json:"userAgent"`
}

type MediaMapping struct {
	MediaID  int    `json:"mediaId"`
	SourceID string `json:"sourceId"`
	Title    string `json:"title"`
}

type StreamPreference struct {
	MediaID int    `json:"mediaId"`
	Value   string `json:"value"`
}

type ContinueEntry struct {
	Title              string            `json:"title"`
	SourceID           string            `json:"sourceId"`
	SourceName         string            `json:"sourceName"`
	MediaURL           string            `json:"mediaUrl"`
	EpisodeURL         string            `json:"episodeUrl"`
	EpisodeName        string            `json:"episodeName"`
	EpisodeNumber      string            `json:"episodeNumber"`
	VideoURL           string            `json:"videoUrl"`
	Quality            string            `json:"quality"`
	Headers            map[string]string `json:"headers,omitempty"`
	Subtitles          []string          `json:"subtitles,omitempty"`
	UpdatedAt          int64             `json:"updatedAt"`
	Position           float64           `json:"position,omitempty"`
	Duration           float64           `json:"duration,omitempty"`
	WatchedEpisodes    []string          `json:"watchedEpisodes,omitempty"`
	AniListEntryID     int               `json:"aniListEntryId,omitempty"`
	AniListMediaID     int               `json:"aniListMediaId,omitempty"`
	AniListProgress    int               `json:"aniListProgress,omitempty"`
	AniListTotal       int               `json:"aniListTotal,omitempty"`
	AniListStatus      string            `json:"aniListStatus,omitempty"`
	AniListMediaStatus string            `json:"aniListMediaStatus,omitempty"`
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
	return filepath.Join(directory, "anymex-cli", "config.json"), nil
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (s *Store) Save(config Config) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
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
