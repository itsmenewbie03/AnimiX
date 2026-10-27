package aniskip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultEndpoint = "https://api.aniskip.com/v2/skip-times"

// Interval is a skip range measured in seconds.
type Interval struct {
	Start float64
	End   float64
}

// Duration returns the length of the interval in seconds.
func (i Interval) Duration() float64 {
	return i.End - i.Start
}

// Times holds the skip ranges for a single episode.
type Times struct {
	Op    *Interval
	Ed    *Interval
	Recap *Interval
}

// Empty reports whether no skip ranges are known.
func (t Times) Empty() bool {
	return t.Op == nil && t.Ed == nil && t.Recap == nil
}

// Client talks to the AniSkip API.
type Client struct {
	http     *http.Client
	endpoint string
}

// NewClient returns an AniSkip client using the public API.
func NewClient() *Client {
	return &Client{
		http:     &http.Client{Timeout: 15 * time.Second},
		endpoint: defaultEndpoint,
	}
}

// Times fetches opening, ending, and recap ranges for an episode. episodeLength
// may be zero when the duration is not known yet, matching AnymeX's request.
func (c *Client) Times(ctx context.Context, malID int, episode float64, episodeLength int) (Times, error) {
	if malID <= 0 || episode <= 0 {
		return Times{}, fmt.Errorf("aniskip: MAL ID and episode number are required")
	}
	number := strconv.FormatFloat(episode, 'f', -1, 64)
	url := fmt.Sprintf("%s/%d/%s?types[]=op&types[]=ed&types[]=recap&types[]=mixed-op&types[]=mixed-ed&episodeLength=%d",
		c.endpoint, malID, number, max(0, episodeLength))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Times{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Times{}, fmt.Errorf("aniskip request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return Times{}, fmt.Errorf("read aniskip response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return Times{}, fmt.Errorf("aniskip HTTP %s", response.Status)
	}
	var payload struct {
		Found   bool `json:"found"`
		Results []struct {
			Type     string `json:"skipType"`
			Interval struct {
				Start float64 `json:"startTime"`
				End   float64 `json:"endTime"`
			} `json:"interval"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Times{}, fmt.Errorf("decode aniskip response: %w", err)
	}
	if !payload.Found {
		return Times{}, nil
	}
	var times Times
	for _, result := range payload.Results {
		interval := &Interval{Start: result.Interval.Start, End: result.Interval.End}
		if interval.End <= interval.Start {
			continue
		}
		switch strings.ToLower(result.Type) {
		case "op", "mixed-op":
			if times.Op == nil {
				times.Op = interval
			}
		case "ed", "mixed-ed":
			if times.Ed == nil {
				times.Ed = interval
			}
		case "recap":
			if times.Recap == nil {
				times.Recap = interval
			}
		}
	}
	return times, nil
}
