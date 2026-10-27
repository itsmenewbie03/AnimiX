package catalog

import (
	"encoding/json"
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		base string
		raw  string
		want string
	}{
		{name: "relative to source", base: "https://example.com/", raw: "https://example.com/anime/1", want: "/anime/1"},
		{name: "protocol relative", raw: "//cdn.example/video.m3u8", want: "https://cdn.example/video.m3u8"},
		{name: "already relative", base: "https://example.com", raw: "/manga/1", want: "/manga/1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeURL(test.base, test.raw); got != test.want {
				t.Fatalf("sanitizeURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPagesDecodesEmbeddedExtensionError(t *testing.T) {
	t.Parallel()
	var pages Pages
	if err := json.Unmarshal([]byte(`{"list":[],"hasNextPage":false,"error":"rate limited"}`), &pages); err != nil {
		t.Fatal(err)
	}
	if pages.Error != "rate limited" {
		t.Fatalf("error = %q", pages.Error)
	}
}

func TestEpisodeNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value any
		want  string
	}{
		{value: "12.5", want: "12.5"},
		{value: float64(12), want: "12"},
		{value: nil, want: ""},
	}
	for _, test := range tests {
		if got := (Episode{EpisodeNumber: test.value}).Number(); got != test.want {
			t.Errorf("Number(%v) = %q, want %q", test.value, got, test.want)
		}
	}
}
