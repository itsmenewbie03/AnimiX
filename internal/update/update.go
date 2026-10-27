package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/itsmenewbie03/AnimiX/releases/latest"
	installerURL     = "https://raw.githubusercontent.com/itsmenewbie03/AnimiX/%s/install.sh"
)

type Release struct {
	Version string
	URL     string
}

type Client struct {
	httpClient *http.Client
	apiURL     string
	installURL string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiURL:     latestReleaseURL,
		installURL: installerURL,
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

func (c *Client) Install(ctx context.Context, release Release) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("automatic updates require a POSIX shell; install %s manually", release.URL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(c.installURL, release.Version), nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download installer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download installer: HTTP %s", response.Status)
	}
	directory, err := os.MkdirTemp("", "animix-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	script := filepath.Join(directory, "install.sh")
	file, err := os.OpenFile(script, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 1<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "bash", script, "--version", release.Version, "--dir", filepath.Dir(executable))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("install %s: %w: %s", release.Version, err, strings.TrimSpace(string(output)))
	}
	return nil
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
