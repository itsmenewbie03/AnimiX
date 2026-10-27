package anilist

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLibraryAndUpdate(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1:
			fmt.Fprint(response, `{"data":{"Viewer":{"id":7,"name":"tester"}}}`)
		case 2:
			fmt.Fprint(response, `{"data":{"anime":{"lists":[{"entries":[{"id":10,"status":"CURRENT","progress":3,"media":{"id":99,"type":"ANIME","status":"RELEASING","title":{"userPreferred":"Example"},"episodes":12,"chapters":null,"nextAiringEpisode":{"episode":7,"airingAt":1800000000},"startDate":{"year":2026,"month":10,"day":1},"endDate":{"year":2027,"month":3,"day":4}}}]}]},"manga":{"lists":[]}}}`)
		case 3:
			fmt.Fprint(response, `{"data":{"SaveMediaListEntry":{"id":10,"status":"COMPLETED","progress":12}}}`)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.endpoint = server.URL
	library, err := client.Library(context.Background(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if library.Viewer.Name != "tester" || len(library.Entries) != 1 || library.Entries[0].Total != 12 || library.Entries[0].Available != 6 {
		t.Fatalf("library = %#v", library)
	}
	entry := library.Entries[0]
	if entry.MediaStatus != "RELEASING" || entry.NextAiringEpisode == nil || entry.NextAiringEpisode.AiringAt != 1800000000 {
		t.Fatalf("release metadata = %#v", entry)
	}
	if entry.EndDate == nil || entry.EndDate.Year() != 2027 || entry.EndDate.Month() != time.March || entry.EndDate.Day() != 4 {
		t.Fatalf("end date = %v", entry.EndDate)
	}
	if entry.StartDate == nil || entry.StartDate.Year() != 2026 || entry.StartDate.Month() != time.October || entry.StartDate.Day() != 1 {
		t.Fatalf("start date = %v", entry.StartDate)
	}
	entry.Progress, entry.Status = 12, "COMPLETED"
	updated, err := client.Update(context.Background(), "test-token", entry)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Progress != 12 || updated.Status != "COMPLETED" {
		t.Fatalf("updated = %#v", updated)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestEpisodeMetadata(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("anilist_id"); got != "116674" {
			t.Errorf("anilist_id = %q", got)
		}
		fmt.Fprint(response, `{"mappings":{"mal_id":16498},"episodes":{"10":{"title":{"en":"The Battle","ja":"ザ・バトル"},"overview":"Episode overview","image":"https://example.com/10.jpg"},"S1":{"title":{"en":"Special"}}}}`)
	}))
	defer server.Close()

	client := NewClient()
	client.episodeMetadataEndpoint = server.URL
	catalog, err := client.EpisodeMetadata(context.Background(), 116674)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.MALID != 16498 {
		t.Fatalf("MAL ID = %d", catalog.MALID)
	}
	if got := catalog.Episodes["10"]; got.Title != "The Battle" || got.Description != "Episode overview" || got.Image != "https://example.com/10.jpg" {
		t.Fatalf("episode metadata = %#v", got)
	}
}

func TestGraphQLError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"data":null,"errors":[{"message":"Invalid token"}]}`)
	}))
	defer server.Close()
	client := NewClient()
	client.endpoint = server.URL
	_, err := client.Library(context.Background(), "bad")
	if err == nil || !strings.Contains(err.Error(), "Invalid token") {
		t.Fatalf("error = %v", err)
	}
}

func TestLibraryMatchesAnymeXUpdatedTimeOrder(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			fmt.Fprint(response, `{"data":{"Viewer":{"id":7,"name":"tester"}}}`)
			return
		}
		fmt.Fprint(response, `{"data":{"anime":{"lists":[{"entries":[
			{"id":1,"status":"CURRENT","progress":1,"updatedAt":100,"media":{"id":101,"idMal":7001,"type":"ANIME","status":"RELEASING","title":{"userPreferred":"Older"},"episodes":12}},
			{"id":2,"status":"CURRENT","progress":2,"updatedAt":200,"media":{"id":102,"idMal":7002,"type":"ANIME","status":"RELEASING","title":{"userPreferred":"Newer"},"episodes":12}}
		]}]},"manga":{"lists":[]}}}`)
	}))
	defer server.Close()

	client := NewClient()
	client.endpoint = server.URL
	library, err := client.Library(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Entries) != 2 || library.Entries[0].Title != "Newer" || library.Entries[1].Title != "Older" {
		t.Fatalf("entries = %#v", library.Entries)
	}
	if library.Entries[0].MALID != 7002 {
		t.Fatalf("MAL ID = %d", library.Entries[0].MALID)
	}
}
