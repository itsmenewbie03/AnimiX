package catalog

import (
	"encoding/json"
	"testing"
)

func TestEpisodeDecodesRuntimeSnakeCaseNumber(t *testing.T) {
	t.Parallel()
	var episode Episode
	if err := json.Unmarshal([]byte(`{"name":"Episode 10","episodeNumber":null,"episode_number":10.0}`), &episode); err != nil {
		t.Fatal(err)
	}
	if got := episode.Number(); got != "10" {
		t.Fatalf("episode number = %q, want 10", got)
	}
}

func TestEpisodeStillDecodesCamelCaseNumber(t *testing.T) {
	t.Parallel()
	var episode Episode
	if err := json.Unmarshal([]byte(`{"name":"Episode 3","episodeNumber":"3"}`), &episode); err != nil {
		t.Fatal(err)
	}
	if got := episode.Number(); got != "3" {
		t.Fatalf("episode number = %q, want 3", got)
	}
}
