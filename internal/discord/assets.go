package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type cachedAsset struct {
	image   string
	expires time.Time
}

type artworkError struct{ status int }

func (e artworkError) Error() string { return "artwork conversion unavailable" }

func assetResolver(client *http.Client) func(context.Context, Settings, string) (string, error) {
	return func(ctx context.Context, settings Settings, url string) (string, error) {
		if !settings.Enabled || !settings.ShowArtwork || settings.Token == "" || settings.ApplicationID == "" || !publicImage(url) {
			return "", errors.New("artwork unavailable")
		}
		body, _ := json.Marshal(map[string][]string{"urls": {url}})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://discord.com/api/v9/applications/"+settings.ApplicationID+"/external-assets", bytes.NewReader(body))
		if err != nil {
			return "", errors.New("invalid artwork request")
		}
		request.Header.Set("Authorization", settings.Token)
		request.Header.Set("Content-Type", "application/json")
		// This private user-session endpoint is isolated from the Gateway. Do not
		// follow redirects with credentials or delay text presence on a lookup.
		copyClient := *client
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := copyClient.Do(request)
		if err != nil {
			return "", errors.New("artwork request failed")
		}
		defer response.Body.Close()
		var result []struct {
			Path string `json:"external_asset_path"`
		}
		if response.StatusCode != http.StatusOK {
			return "", artworkError{status: response.StatusCode}
		}
		if json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result) != nil || len(result) == 0 || result[0].Path == "" || len(result[0].Path) > 2048 {
			return "", artworkError{status: response.StatusCode}
		}
		return "mp:" + strings.TrimPrefix(result[0].Path, "mp:"), nil
	}
}
