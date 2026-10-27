package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseAssetURL = "https://github.com/itsmenewbie03/AnimiX/releases/download/%s/%s"

type ProbePhase string

const (
	ProbeDownloading ProbePhase = "Downloading release"
	ProbeChecksums   ProbePhase = "Downloading checksums"
	ProbeVerifying   ProbePhase = "Verifying checksum"
	ProbeReplacing   ProbePhase = "Replacing executable"
	ProbeComplete    ProbePhase = "Download verified"
)

type ProbeProgress struct {
	Phase      ProbePhase
	Downloaded int64
	Total      int64
}

func (progress ProbeProgress) Fraction() float64 {
	if progress.Total <= 0 {
		return 0
	}
	return min(1, float64(progress.Downloaded)/float64(progress.Total))
}

// Probe downloads and verifies the release for this platform, then discards
// the staged binary. It intentionally performs no installation or replacement.
func (c *Client) Probe(ctx context.Context, release Release, report func(ProbeProgress)) error {
	if report == nil {
		report = func(ProbeProgress) {}
	}
	asset := probeAssetName(runtime.GOOS, runtime.GOARCH)
	directory, err := os.MkdirTemp("", "animix-update-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, asset)

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := c.downloadProbeAsset(ctx, release, asset, file, report); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	report(ProbeProgress{Phase: ProbeChecksums})
	expected, err := c.downloadProbeChecksum(ctx, release, asset)
	if err != nil {
		return err
	}
	report(ProbeProgress{Phase: ProbeVerifying})
	if err := verifyProbeChecksum(path, expected); err != nil {
		return err
	}
	report(ProbeProgress{Phase: ProbeComplete})
	return nil
}

// InstallWithProgress uses the probe's measured download and verification
// pipeline, then atomically replaces the current executable on Unix systems.
func (c *Client) InstallWithProgress(ctx context.Context, release Release, report func(ProbeProgress)) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("automatic updates cannot replace a running executable on Windows; install %s manually", release.URL)
	}
	if report == nil {
		report = func(ProbeProgress) {}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	asset := probeAssetName(runtime.GOOS, runtime.GOARCH)
	staged, err := os.CreateTemp(filepath.Dir(executable), ".animix-update-*")
	if err != nil {
		return fmt.Errorf("stage update: %w", err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if err := c.downloadProbeAsset(ctx, release, asset, staged, report); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	report(ProbeProgress{Phase: ProbeChecksums})
	expected, err := c.downloadProbeChecksum(ctx, release, asset)
	if err != nil {
		return err
	}
	report(ProbeProgress{Phase: ProbeVerifying})
	if err := verifyProbeChecksum(stagedPath, expected); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	report(ProbeProgress{Phase: ProbeReplacing})
	if err := os.Rename(stagedPath, executable); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	report(ProbeProgress{Phase: ProbeComplete})
	return nil
}

func (c *Client) downloadProbeAsset(
	ctx context.Context,
	release Release,
	asset string,
	target *os.File,
	report func(ProbeProgress),
) error {
	response, err := c.probeRequest(ctx, fmt.Sprintf(c.assetURL, release.Version, asset))
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	defer response.Body.Close()
	progress := ProbeProgress{Phase: ProbeDownloading, Total: response.ContentLength}
	report(progress)
	buffer := make([]byte, 64*1024)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			written, writeErr := target.Write(buffer[:read])
			progress.Downloaded += int64(written)
			report(progress)
			if writeErr != nil {
				return writeErr
			}
			if written != read {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if progress.Total > 0 && progress.Downloaded != progress.Total {
		return fmt.Errorf("downloaded %d of %d bytes", progress.Downloaded, progress.Total)
	}
	return nil
}

func (c *Client) downloadProbeChecksum(ctx context.Context, release Release, asset string) (string, error) {
	response, err := c.probeRequest(ctx, fmt.Sprintf(c.assetURL, release.Version, "SHA256SUMS"))
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[1] == asset || fields[1] == "*"+asset) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}

func (c *Client) probeRequest(ctx context.Context, URL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "animix-update-probe")
	client := *c.httpClient
	client.Timeout = 2 * time.Minute
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	return response, nil
}

func verifyProbeChecksum(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func probeAssetName(goos, goarch string) string {
	name := "animix_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}
