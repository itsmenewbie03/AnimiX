// Package discord publishes the current AnimiX activity through a direct Discord
// Gateway connection. Connection recovery never changes the desired activity.
package discord

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

type Kind uint8

const (
	Watching Kind = iota + 1
	Reading
)

// Snapshot is an immutable observation. URLs are optional public metadata, never
// stream URLs, source cookies, or loopback reader URLs.
type Snapshot struct {
	Kind                     Kind
	Title, Part, Number      string
	Total                    int
	AniListMediaID           int
	CoverURL                 string
	Position, Duration, Rate float64
	Paused, Buffering        bool
	Active                   bool
	Page, Pages              int
	ObservedAt               time.Time
	// ValidUntil bounds reader liveness and stale player observations. A zero
	// deadline is useful for static observations, including tests.
	ValidUntil time.Time
}

type activity struct {
	ApplicationID string          `json:"application_id,omitempty"`
	Name          string          `json:"name"`
	Type          int             `json:"type"`
	Details       string          `json:"details"`
	State         string          `json:"state,omitempty"`
	Timestamps    *timestamps     `json:"timestamps,omitempty"`
	Assets        *assets         `json:"assets,omitempty"`
	Buttons       []string        `json:"buttons,omitempty"`
	Metadata      *buttonMetadata `json:"metadata,omitempty"`
}

type timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type assets struct {
	LargeImage string `json:"large_image"`
	LargeText  string `json:"large_text"`
	LargeURL   string `json:"large_url,omitempty"`
}

// The user-session wire format follows the AnymeX Gateway path. Keep it here,
// separate from observations and transport; this is not the bot/OAuth interface.
type buttonMetadata struct {
	URLs []string `json:"button_urls"`
}

func formatActivity(settings Settings, snapshot Snapshot, image string, now time.Time) *activity {
	active := snapshot.Active && (snapshot.ValidUntil.IsZero() || now.Before(snapshot.ValidUntil))
	if !active {
		if !settings.ShowBrowsing {
			return nil
		}
		return &activity{Name: "AnimiX", ApplicationID: settings.ApplicationID, Details: "Browsing AnimiX"}
	}
	result := &activity{Name: "AnimiX", ApplicationID: settings.ApplicationID, Details: shortText(snapshot.Title, 128)}
	if result.Details == "" {
		result.Details = "AnimiX"
	}
	part := snapshot.Part
	if snapshot.Kind == Reading {
		if snapshot.Number != "" {
			part = "Chapter " + snapshot.Number
		}
		result.State = "Reading"
		if part != "" {
			result.State += " · " + part
		}
		if snapshot.Page > 0 && snapshot.Pages > 0 {
			result.State += fmt.Sprintf(" · Page %d/%d", snapshot.Page, snapshot.Pages)
		}
	} else {
		result.Type = 3 // Watching
		if snapshot.Number != "" {
			part = "Episode " + snapshot.Number
			if snapshot.Total > 0 {
				part += fmt.Sprintf("/%d", snapshot.Total)
			}
		}
		result.State = part
		switch {
		case snapshot.Paused:
			result.State = "Paused · " + part
		case snapshot.Buffering:
			result.State = "Buffering · " + part
		default:
			result.Timestamps = playbackTimestamps(snapshot, now)
		}
	}
	result.State = shortText(strings.Trim(result.State, " ·"), 128)
	if settings.ApplicationID != "" {
		aniListURL := ""
		if snapshot.AniListMediaID > 0 {
			mediaType := "anime"
			if snapshot.Kind == Reading {
				mediaType = "manga"
			}
			aniListURL = fmt.Sprintf("https://anilist.co/%s/%d", mediaType, snapshot.AniListMediaID)
		}
		if image == "" {
			image = settings.LargeImage
		}
		if image != "" {
			result.Assets = &assets{LargeImage: image, LargeText: result.Details, LargeURL: aniListURL}
		}
		if aniListURL != "" {
			result.Buttons = []string{"View on AniList"}
			result.Metadata = &buttonMetadata{URLs: []string{aniListURL}}
		}
	}
	return result
}

func playbackTimestamps(snapshot Snapshot, now time.Time) *timestamps {
	if !finite(snapshot.Position) || snapshot.Position < 0 || !finite(snapshot.Rate) || snapshot.Rate <= 0 || snapshot.ObservedAt.IsZero() {
		return nil
	}
	position := snapshot.Position + max(0, now.Sub(snapshot.ObservedAt).Seconds())*snapshot.Rate
	if snapshot.Duration > 0 && finite(snapshot.Duration) {
		position = min(position, snapshot.Duration)
	}
	// Discord Gateway activity timestamps use milliseconds, unlike some local
	// RPC libraries. Guard durations before converting float64 to time.Duration.
	elapsed := position / snapshot.Rate
	if elapsed > (365 * 24 * time.Hour).Seconds() {
		return nil
	}
	result := &timestamps{Start: now.Add(-time.Duration(elapsed * float64(time.Second))).UnixMilli()}
	if snapshot.Duration > 0 && finite(snapshot.Duration) {
		remaining := max(0, snapshot.Duration-position) / snapshot.Rate
		if remaining < (365 * 24 * time.Hour).Seconds() {
			result.End = now.Add(time.Duration(remaining * float64(time.Second))).UnixMilli()
		}
	}
	return result
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func shortText(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}

func publicImage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 2048 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host != "localhost" && !strings.HasSuffix(host, ".localhost") && !strings.Contains(host, ":") && !isIP(host)
}

func presencePayload(value *activity) []byte {
	activities := []*activity{}
	if value != nil {
		copy := *value
		activities = append(activities, &copy)
	}
	payload := struct {
		Op int `json:"op"`
		D  struct {
			Since      *int        `json:"since"`
			Activities []*activity `json:"activities"`
			Status     string      `json:"status"`
			AFK        bool        `json:"afk"`
		} `json:"d"`
	}{Op: 3, D: struct {
		Since      *int        `json:"since"`
		Activities []*activity `json:"activities"`
		Status     string      `json:"status"`
		AFK        bool        `json:"afk"`
	}{Activities: activities, Status: "online"}}
	data, _ := json.Marshal(payload)
	// Escaping can expand otherwise bounded metadata. An oversized optional
	// asset must not take text presence (or the whole connection) offline.
	if len(data) > 4096 && len(activities) != 0 && activities[0].Assets != nil {
		activities[0].Assets = nil
		data, _ = json.Marshal(payload)
	}
	return data
}
