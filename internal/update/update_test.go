package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
