package update

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

const latestReleaseURL = "https://api.github.com/repos/itsmenewbie03/AnimiX/releases/latest"

type Release struct {
	Version string
	URL     string
}

type Client struct {
	httpClient *http.Client
	apiURL     string
	assetURL   string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiURL:     latestReleaseURL,
		assetURL:   releaseAssetURL,
	}
}

func (c *Client) Check(ctx context.Context, current string) (Release, error) {
	if current == "" || current == "dev" {
		return Release{}, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "animix-update-check")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("check updates: HTTP %s", response.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Release{}, err
	}
	if payload.TagName == "" || compareVersions(payload.TagName, current) <= 0 {
		return Release{}, nil
	}
	return Release{Version: payload.TagName, URL: payload.HTMLURL}, nil
}

func compareVersions(left, right string) int {
	parse := func(value string) []int {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		result := make([]int, len(parts))
		for index, part := range parts {
			result[index], _ = strconv.Atoi(part)
		}
		return result
	}
	a, b := parse(left), parse(right)
	length := max(len(a), len(b))
	for index := 0; index < length; index++ {
		var av, bv int
		if index < len(a) {
			av = a[index]
		}
		if index < len(b) {
			bv = b[index]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
