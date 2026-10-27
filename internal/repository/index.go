package repository

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Extension struct {
	Name      string
	Package   string
	APK       string
	APKURL    string
	IconURL   string
	Language  string
	Version   string
	NSFW      bool
	Type      string
	SourceIDs []string
	RepoURL   string
}

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 45 * time.Second}}
}

func (c *Client) Fetch(repoURL, itemType string) ([]Extension, error) {
	request, err := http.NewRequest(http.MethodGet, repoURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, application/octet-stream")
	request.Header.Set("User-Agent", "AnimiX")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch repository: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch repository: HTTP %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024*1024))
	if err != nil {
		return nil, err
	}
	return Parse(data, repoURL, itemType)
}

func Parse(data []byte, repoURL, itemType string) ([]Extension, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("open gzip repository: %w", err)
		}
		decompressed, err := io.ReadAll(io.LimitReader(reader, 64*1024*1024))
		closeErr := reader.Close()
		if err != nil {
			return nil, fmt.Errorf("decompress repository: %w", err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		data = decompressed
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty repository index")
	}
	trimmed := bytes.TrimSpace(data)
	var raw []rawExtension
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, fmt.Errorf("parse JSON repository: %w", err)
		}
	} else {
		var err error
		raw, err = decodeProtobufIndex(data)
		if err != nil {
			return nil, fmt.Errorf("parse protobuf repository: %w", err)
		}
	}

	result := make([]Extension, 0, len(raw))
	for _, item := range raw {
		typeName := detectType(item, itemType)
		if typeName != itemType {
			continue
		}
		language := item.Language
		ids := make([]string, 0, len(item.Sources))
		for _, source := range item.Sources {
			ids = append(ids, fmt.Sprint(source.ID))
			if language == "" {
				language = source.Language
			}
		}
		name := strings.TrimPrefix(strings.TrimPrefix(item.Name, "Aniyomi: "), "Tachiyomi: ")
		result = append(result, Extension{
			Name:      name,
			Package:   item.Package,
			APK:       firstNonEmpty(item.APK, path.Base(item.APKURL)),
			APKURL:    resolveAssetURL(repoURL, firstNonEmpty(item.APKURL, item.APK), "apk"),
			IconURL:   resolveAssetURL(repoURL, item.IconURL, "icon"),
			Language:  firstNonEmpty(language, "en"),
			Version:   item.Version,
			NSFW:      item.NSFW || item.NSFWLevel > 0,
			Type:      typeName,
			SourceIDs: ids,
			RepoURL:   repoURL,
		})
	}
	return result, nil
}

type rawExtension struct {
	Name        string      `json:"name"`
	Package     string      `json:"pkg"`
	APK         string      `json:"apk"`
	APKURL      string      `json:"apkUrl"`
	IconURL     string      `json:"iconUrl"`
	Language    string      `json:"lang"`
	Version     string      `json:"version"`
	NSFW        bool        `json:"isNsfw"`
	NSFWLevel   int         `json:"nsfw"`
	Sources     []rawSource `json:"sources"`
	VersionCode uint64      `json:"code"`
}

type rawSource struct {
	ID       any    `json:"id"`
	Name     string `json:"name"`
	Language string `json:"lang"`
	BaseURL  string `json:"baseUrl"`
}

func detectType(extension rawExtension, fallback string) string {
	if strings.HasPrefix(extension.Name, "Aniyomi: ") || strings.Contains(extension.Package, ".anime.") {
		return "anime"
	}
	if strings.HasPrefix(extension.Name, "Tachiyomi: ") || strings.Contains(extension.Package, ".manga.") {
		return "manga"
	}
	return fallback
}

func resolveAssetURL(repoURL, asset, folder string) string {
	if asset == "" {
		return ""
	}
	parsed, err := url.Parse(asset)
	if err == nil && parsed.IsAbs() {
		return asset
	}
	base := repoURL
	for _, suffix := range []string{"/index.min.json", "/index.json", "/index.pb.gz", "/index.pb"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return strings.TrimRight(base, "/") + "/" + folder + "/" + path.Base(asset)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
