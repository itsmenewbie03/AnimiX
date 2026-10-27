package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestProbeReportsMeasuredDownloadAndVerifiesChecksum(t *testing.T) {
	t.Parallel()
	asset := []byte("verified animix release")
	digest := fmt.Sprintf("%x", sha256.Sum256(asset))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v-test/SHA256SUMS" {
			_, _ = fmt.Fprintf(writer, "%s  %s\n", digest, probeAssetName(runtime.GOOS, runtime.GOARCH))
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(asset)))
		_, _ = writer.Write(asset)
	}))
	defer server.Close()
	client := NewClient()
	client.assetURL = server.URL + "/%s/%s"
	var reports []ProbeProgress
	if err := client.Probe(context.Background(), Release{Version: "v-test"}, func(progress ProbeProgress) {
		reports = append(reports, progress)
	}); err != nil {
		t.Fatal(err)
	}
	if len(reports) < 4 || reports[len(reports)-1].Phase != ProbeComplete {
		t.Fatalf("progress reports = %#v", reports)
	}
	foundMeasuredDownload := false
	for _, report := range reports {
		if report.Phase == ProbeDownloading && report.Downloaded == int64(len(asset)) && report.Total == int64(len(asset)) {
			foundMeasuredDownload = true
		}
	}
	if !foundMeasuredDownload {
		t.Fatalf("missing measured download in %#v", reports)
	}
}

func TestCheckReturnsNewerRelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example.test/release"}`))
	}))
	defer server.Close()
	client := NewClient()
	client.apiURL = server.URL

	release, err := client.Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "v0.2.0" || release.URL != "https://example.test/release" {
		t.Fatalf("release = %#v", release)
	}
}

func TestCheckSkipsDevelopmentAndCurrentBuilds(t *testing.T) {
	t.Parallel()
	client := NewClient()
	if release, err := client.Check(context.Background(), "dev"); err != nil || release.Version != "" {
		t.Fatalf("development check = %#v, %v", release, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"tag_name":"v0.1.0"}`))
	}))
	defer server.Close()
	client.apiURL = server.URL
	if release, err := client.Check(context.Background(), "v0.1.0"); err != nil || release.Version != "" {
		t.Fatalf("current check = %#v, %v", release, err)
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"v0.2.0", "v0.1.0", 1},
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.0", "v0.2.0", -1},
		{"v1.0.1", "v1.0.0", 1},
	} {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}
