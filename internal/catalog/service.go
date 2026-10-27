package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/itsmenewbie03/animix/internal/bridge"
)

type Service struct {
	bridge *bridge.Client
}

func NewService(client *bridge.Client) *Service {
	return &Service{bridge: client}
}

func (s *Service) Sources(ctx context.Context, extensionDirectory string) ([]Source, error) {
	var sources []Source
	err := s.bridge.Call(ctx, "loadExtensions", map[string]any{
		"folderPath": extensionDirectory,
	}, &sources)
	return sources, err
}

func (s *Service) Search(ctx context.Context, source Source, query string, page int) (Pages, error) {
	var result Pages
	err := s.bridge.Call(ctx, "search", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
		"query":    query,
		"page":     page,
		"filters":  []any{},
	}, &result)
	if err == nil && result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func (s *Service) Details(ctx context.Context, source Source, media Media) (Media, error) {
	var result Media
	err := s.bridge.Call(ctx, "getDetail", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
		"media": map[string]any{
			"title":         media.DisplayTitle(),
			"url":           sanitizeURL(source.BaseURL, media.URL),
			"thumbnail_url": firstNonEmpty(media.Thumbnail, media.Cover),
			"description":   media.Description,
			"author":        media.Author,
			"artist":        media.Artist,
			"genre":         media.Genres,
		},
	}, &result)
	return result, err
}

func (s *Service) Videos(ctx context.Context, source Source, episode Episode) ([]Video, error) {
	var result []Video
	err := s.bridge.Call(ctx, "getVideoList", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
		"episode":  episodePayload(source, episode),
	}, &result)
	return result, err
}

func (s *Service) Pages(ctx context.Context, source Source, episode Episode) ([]Page, error) {
	var result []Page
	err := s.bridge.Call(ctx, "getPageList", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
		"episode":  episodePayload(source, episode),
	}, &result)
	return result, err
}

func (s *Service) ConvertAPK(ctx context.Context, apkPath, outputPath string) error {
	return s.bridge.Call(ctx, "convertApk", map[string]any{
		"apkPath":    apkPath,
		"outJarPath": outputPath,
	}, nil)
}

func (s *Service) UnloadExtension(ctx context.Context, sourceID string) error {
	return s.bridge.Call(ctx, "unloadExtension", map[string]any{
		"sourceId": sourceID,
	}, nil)
}

func (s *Service) SetCookies(ctx context.Context, url, cookieString string) error {
	var result networkStateResult
	if err := s.bridge.Call(ctx, "setCookies", map[string]any{
		"url": url, "cookieString": cookieString,
	}, &result); err != nil {
		return err
	}
	return result.err("set cookies")
}

func (s *Service) SetUserAgent(ctx context.Context, url, userAgent string) error {
	var result networkStateResult
	if err := s.bridge.Call(ctx, "setUserAgent", map[string]any{
		"url": url, "userAgent": userAgent,
	}, &result); err != nil {
		return err
	}
	return result.err("set User-Agent")
}

type networkStateResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func (r networkStateResult) err(operation string) error {
	if r.Success {
		return nil
	}
	if r.Error == "" {
		r.Error = "runtime rejected the update"
	}
	return fmt.Errorf("%s: %s", operation, r.Error)
}

func episodePayload(source Source, episode Episode) map[string]any {
	return map[string]any{
		"name":           episode.Name,
		"url":            sanitizeURL(source.BaseURL, episode.URL),
		"date_upload":    episode.DateUpload,
		"description":    episode.Description,
		"episode_number": episode.Number(),
		"scanlator":      episode.Scanlator,
		"memo":           firstNonEmpty(episode.Memo, episode.Description),
	}
}

func sanitizeURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" && strings.HasPrefix(raw, baseURL) {
		raw = strings.TrimPrefix(raw, baseURL)
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
