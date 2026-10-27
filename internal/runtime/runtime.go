package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SourceUpstream = "upstream"
	SourceFork     = "fork"

	DefaultSource   = SourceUpstream
	DefaultVersion  = "v2.7.3"
	DefaultChecksum = "87ea7e65eec806ae0890f78f3ad9a0f7e516ec84b43a772a83bb9d60b812a535"

	assetName = "anymex_desktop_runtime.jar"
)

var (
	repositories = map[string]string{
		SourceUpstream: "RyanYuuki/AnymeXExtensionRuntimeBridge",
		SourceFork:     "itsmenewbie03/AnymeXExtensionRuntimeBridge",
	}
	httpClient = &http.Client{Timeout: 5 * time.Minute}
)

// Release describes one downloadable runtime build.
type Release struct {
	Source   string
	Version  string
	URL      string
	Checksum string
}

// Fallback returns the last known upstream release used when no runtime has
// been installed or recorded.
func Fallback() Release {
	return Release{
		Source:   DefaultSource,
		Version:  DefaultVersion,
		URL:      releaseURL(repositories[DefaultSource], DefaultVersion),
		Checksum: DefaultChecksum,
	}
}

// Resolve turns a spec such as "fork@latest", "upstream@v2.7.3", "fork", or
// "v2.7.3-animix.1" into a concrete release. An empty spec or "default"
// resolves to the latest upstream release; use "fork@latest" for this fork.
func Resolve(ctx context.Context, spec string) (Release, error) {
	source, version, err := parseSpec(spec)
	if err != nil {
		return Release{}, err
	}
	return fetchRelease(ctx, source, version)
}

// Path returns the install location for a runtime source and version.
func Path(source, version string) (string, error) {
	directory, err := runtimeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, source, version, assetName), nil
}

// DefaultPath returns the last installed runtime, falling back to the last
// known upstream release when no valid selection has been recorded.
func DefaultPath() (string, error) {
	if source, version, ok := currentSelection(); ok {
		if path, err := Path(source, version); err == nil {
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				return path, nil
			}
		}
	}
	fallback := Fallback()
	return Path(fallback.Source, fallback.Version)
}

// SetCurrent records source and version as the runtime used by default.
func SetCurrent(source, version string) error {
	directory, err := runtimeDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary := filepath.Join(directory, "current.tmp")
	if err := os.WriteFile(temporary, []byte(source+"\n"+version+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(directory, "current"))
}

func runtimeDirectory() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "animix", "runtime"), nil
}

func currentSelection() (string, string, bool) {
	directory, err := runtimeDirectory()
	if err != nil {
		return "", "", false
	}
	data, err := os.ReadFile(filepath.Join(directory, "current"))
	if err != nil {
		return "", "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return "", "", false
	}
	source, version := fields[0], fields[1]
	if _, ok := repositories[source]; !ok {
		return "", "", false
	}
	if version == "" || strings.ContainsAny(version, `/\`) || version == "." || version == ".." {
		return "", "", false
	}
	return source, version, true
}

// Install downloads release to destination and verifies its SHA-256 digest.
func Install(ctx context.Context, release Release, destination string) error {
	if valid, _ := Verify(destination, release.Checksum); valid {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "animix-runtime-installer")
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download runtime: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download runtime: HTTP %s", response.Status)
	}

	temporary := destination + ".download"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	valid, err := Verify(temporary, release.Checksum)
	if err != nil || !valid {
		_ = os.Remove(temporary)
		return fmt.Errorf("runtime checksum mismatch")
	}
	return os.Rename(temporary, destination)
}

// Verify reports whether path matches the expected SHA-256 checksum.
func Verify(path, checksum string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), checksum), nil
}

func parseSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "default") {
		return SourceUpstream, "latest", nil
	}

	source := DefaultSource
	version := ""
	if at := strings.IndexByte(spec, '@'); at >= 0 {
		source = strings.ToLower(strings.TrimSpace(spec[:at]))
		version = strings.TrimSpace(spec[at+1:])
	} else if _, ok := repositories[strings.ToLower(spec)]; ok {
		source = strings.ToLower(spec)
	} else {
		return "", "", fmt.Errorf("invalid runtime spec %q (use source@version, e.g. upstream@v2.7.3 or fork@latest)", spec)
	}
	if source == "" {
		source = DefaultSource
	}
	if _, ok := repositories[source]; !ok {
		return "", "", fmt.Errorf("unknown runtime source %q (want %q or %q)", source, SourceUpstream, SourceFork)
	}
	if version == "" {
		version = "latest"
	}
	if strings.EqualFold(version, "latest") {
		version = "latest"
	} else if strings.ContainsAny(version, `/\`) || version == "." || version == ".." {
		return "", "", fmt.Errorf("invalid runtime version %q", version)
	}
	return source, version, nil
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func fetchRelease(ctx context.Context, source, version string) (Release, error) {
	repository := repositories[source]
	endpoint := "https://api.github.com/repos/" + repository + "/releases/latest"
	if version != "latest" {
		endpoint = "https://api.github.com/repos/" + repository + "/releases/tags/" + url.PathEscape(version)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "animix-runtime-installer")
	response, err := httpClient.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("resolve %s runtime: %w", source, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("resolve %s runtime %s: HTTP %s", source, version, response.Status)
	}

	var payload githubRelease
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decode %s runtime release: %w", source, err)
	}
	if payload.TagName == "" {
		return Release{}, fmt.Errorf("resolve %s runtime: release has no tag", source)
	}

	var asset *githubAsset
	for index := range payload.Assets {
		if payload.Assets[index].Name == assetName {
			asset = &payload.Assets[index]
			break
		}
	}
	if asset == nil {
		return Release{}, fmt.Errorf("release %s has no %s asset", payload.TagName, assetName)
	}
	checksum, err := assetChecksum(ctx, payload.Assets, asset)
	if err != nil {
		return Release{}, err
	}
	return Release{
		Source:   source,
		Version:  payload.TagName,
		URL:      asset.BrowserDownloadURL,
		Checksum: checksum,
	}, nil
}

func assetChecksum(ctx context.Context, assets []githubAsset, asset *githubAsset) (string, error) {
	if digest := strings.TrimPrefix(asset.Digest, "sha256:"); validSHA256(digest) {
		return strings.ToLower(digest), nil
	}
	for _, candidate := range assets {
		if candidate.Name != assetName+".sha256" {
			continue
		}
		body, err := downloadText(ctx, candidate.BrowserDownloadURL)
		if err != nil {
			return "", fmt.Errorf("download runtime checksum: %w", err)
		}
		fields := strings.Fields(body)
		if len(fields) > 0 && validSHA256(fields[0]) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("release asset %s has no SHA-256 checksum", assetName)
}

func downloadText(ctx context.Context, address string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "animix-runtime-installer")
	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func releaseURL(repository, version string) string {
	return "https://github.com/" + repository + "/releases/download/" + url.PathEscape(version) + "/" + assetName
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
