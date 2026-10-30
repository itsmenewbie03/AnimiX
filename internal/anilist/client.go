package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const endpoint = "https://graphql.anilist.co"
const episodeMetadataEndpoint = "https://api.ani.zip/mappings"

const AuthorizeURL = "https://anilist.co/api/v2/oauth/authorize?client_id=55435&response_type=token"

type Client struct {
	http                    *http.Client
	endpoint                string
	episodeMetadataEndpoint string
}

type Viewer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Entry struct {
	ID                int
	MediaID           int
	MALID             int
	CoverURL          string
	Title             string
	EnglishTitle      string
	RomajiTitle       string
	NativeTitle       string
	Synonyms          []string
	Type              string
	Status            string
	MediaStatus       string
	UpdatedAt         int64
	Progress          int
	Total             int
	Available         int
	NextAiringEpisode *NextAiringEpisode
	StartDate         *time.Time
	EndDate           *time.Time
}

type NextAiringEpisode struct {
	Episode  int
	AiringAt int64
}

type Library struct {
	Viewer  Viewer
	Entries []Entry
}

type EpisodeMetadata struct {
	Title       string
	Description string
	Image       string
}

// EpisodeCatalog is the ani.zip metadata for a series, including the MAL ID
// used by AniSkip.
type EpisodeCatalog struct {
	MALID    int
	Episodes map[string]EpisodeMetadata
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second}, endpoint: endpoint,
		episodeMetadataEndpoint: episodeMetadataEndpoint,
	}
}

func (c *Client) EpisodeMetadata(ctx context.Context, mediaID int) (EpisodeCatalog, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?anilist_id=%d", c.episodeMetadataEndpoint, mediaID), nil)
	if err != nil {
		return EpisodeCatalog{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return EpisodeCatalog{}, fmt.Errorf("episode metadata request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return EpisodeCatalog{}, fmt.Errorf("episode metadata HTTP %s", response.Status)
	}
	var payload struct {
		Mappings struct {
			MALID int `json:"mal_id"`
		} `json:"mappings"`
		Episodes map[string]struct {
			Title    map[string]string `json:"title"`
			Overview string            `json:"overview"`
			Summary  string            `json:"summary"`
			Image    string            `json:"image"`
		} `json:"episodes"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&payload); err != nil {
		return EpisodeCatalog{}, fmt.Errorf("decode episode metadata: %w", err)
	}
	result := EpisodeCatalog{
		MALID:    payload.Mappings.MALID,
		Episodes: make(map[string]EpisodeMetadata, len(payload.Episodes)),
	}
	for number, episode := range payload.Episodes {
		result.Episodes[number] = EpisodeMetadata{
			// Use only explicitly English episode titles. Missing titles produce a
			// neutral "Episode N" label instead of a mixed-language media title.
			Title:       strings.TrimSpace(episode.Title["en"]),
			Description: firstNonEmpty(episode.Overview, episode.Summary), Image: episode.Image,
		}
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Client) Library(ctx context.Context, token string) (Library, error) {
	var viewerData struct {
		Viewer Viewer `json:"Viewer"`
	}
	if err := c.call(ctx, token, `query { Viewer { id name } }`, nil, &viewerData); err != nil {
		return Library{}, err
	}
	const query = `
query ($userId: Int!) {
  anime: MediaListCollection(userId: $userId, type: ANIME, sort: UPDATED_TIME) {
    lists { entries { id status progress updatedAt media { id idMal type status title { userPreferred english romaji native } coverImage { extraLarge large medium } synonyms episodes chapters nextAiringEpisode { episode airingAt } startDate { year month day } endDate { year month day } } } }
  }
  manga: MediaListCollection(userId: $userId, type: MANGA, sort: UPDATED_TIME) {
    lists { entries { id status progress updatedAt media { id type title { userPreferred english romaji native } coverImage { extraLarge large medium } synonyms episodes chapters nextAiringEpisode { episode } } } }
  }
}`
	type media struct {
		ID       int    `json:"id"`
		IDMal    int    `json:"idMal"`
		Type     string `json:"type"`
		Status   string `json:"status"`
		Episodes int    `json:"episodes"`
		Chapters int    `json:"chapters"`
		Title    struct {
			UserPreferred string `json:"userPreferred"`
			English       string `json:"english"`
			Romaji        string `json:"romaji"`
			Native        string `json:"native"`
		} `json:"title"`
		CoverImage struct {
			ExtraLarge string `json:"extraLarge"`
			Large      string `json:"large"`
			Medium     string `json:"medium"`
		} `json:"coverImage"`
		Synonyms          []string `json:"synonyms"`
		NextAiringEpisode *struct {
			Episode  int   `json:"episode"`
			AiringAt int64 `json:"airingAt"`
		} `json:"nextAiringEpisode"`
		StartDate fuzzyDate `json:"startDate"`
		EndDate   fuzzyDate `json:"endDate"`
	}
	type rawEntry struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		Progress  int    `json:"progress"`
		UpdatedAt int64  `json:"updatedAt"`
		Media     media  `json:"media"`
	}
	type collection struct {
		Lists []struct {
			Entries []rawEntry `json:"entries"`
		} `json:"lists"`
	}
	var data struct {
		Anime collection `json:"anime"`
		Manga collection `json:"manga"`
	}
	if err := c.call(ctx, token, query, map[string]any{"userId": viewerData.Viewer.ID}, &data); err != nil {
		return Library{}, err
	}
	result := Library{Viewer: viewerData.Viewer}
	appendCollection := func(value collection) {
		entries := make([]rawEntry, 0)
		for _, list := range value.Lists {
			entries = append(entries, list.Entries...)
		}
		for index := len(entries) - 1; index >= 0; index-- {
			item := entries[index]
			total := item.Media.Episodes
			available := 0
			if strings.EqualFold(item.Media.Type, "MANGA") {
				total = item.Media.Chapters
			} else if item.Media.NextAiringEpisode != nil && item.Media.NextAiringEpisode.Episode > 0 {
				available = item.Media.NextAiringEpisode.Episode - 1
			} else if strings.EqualFold(item.Media.Status, "COMPLETED") || strings.EqualFold(item.Media.Status, "FINISHED") {
				available = total
			}
			var next *NextAiringEpisode
			if item.Media.NextAiringEpisode != nil {
				next = &NextAiringEpisode{Episode: item.Media.NextAiringEpisode.Episode, AiringAt: item.Media.NextAiringEpisode.AiringAt}
			}
			result.Entries = append(result.Entries, Entry{
				ID: item.ID, MediaID: item.Media.ID, MALID: item.Media.IDMal, Title: item.Media.Title.UserPreferred,
				CoverURL:     firstNonEmpty(item.Media.CoverImage.ExtraLarge, item.Media.CoverImage.Large, item.Media.CoverImage.Medium),
				EnglishTitle: item.Media.Title.English, RomajiTitle: item.Media.Title.Romaji,
				NativeTitle: item.Media.Title.Native, Synonyms: item.Media.Synonyms,
				Type: item.Media.Type, Status: item.Status, MediaStatus: item.Media.Status, UpdatedAt: item.UpdatedAt,
				Progress: item.Progress, Total: total, Available: available,
				NextAiringEpisode: next, StartDate: item.Media.StartDate.time(), EndDate: item.Media.EndDate.time(),
			})
		}
	}
	appendCollection(data.Anime)
	appendCollection(data.Manga)
	return result, nil
}

type fuzzyDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (d fuzzyDate) time() *time.Time {
	if d.Year == 0 {
		return nil
	}
	month := d.Month
	if month == 0 {
		month = 1
	}
	day := d.Day
	if day == 0 {
		day = 1
	}
	value := time.Date(d.Year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	return &value
}

func (c *Client) Update(ctx context.Context, token string, entry Entry) (Entry, error) {
	const mutation = `
mutation ($id: Int!, $status: MediaListStatus!, $progress: Int!) {
  SaveMediaListEntry(id: $id, status: $status, progress: $progress) {
    id status progress media { id type title { userPreferred } episodes chapters }
  }
}`
	var data struct {
		Saved struct {
			ID       int    `json:"id"`
			Status   string `json:"status"`
			Progress int    `json:"progress"`
		} `json:"SaveMediaListEntry"`
	}
	err := c.call(ctx, token, mutation, map[string]any{
		"id": entry.ID, "status": entry.Status, "progress": entry.Progress,
	}, &data)
	if err != nil {
		return Entry{}, err
	}
	entry.ID, entry.Status, entry.Progress = data.Saved.ID, data.Saved.Status, data.Saved.Progress
	return entry, nil
}

func (c *Client) call(ctx context.Context, token, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("AniList request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("AniList HTTP %s: %s", response.Status, string(body))
	}
	var envelope graphQLResponse[json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("AniList: %s", envelope.Errors[0].Message)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode AniList response: %w", err)
	}
	return nil
}
