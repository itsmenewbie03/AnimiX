package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/mangareader"
)

func TestMangaChapterScanlatorFilters(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "mpv")
	model.width, model.height = 100, 42
	model.selectedSource = catalog.Source{ID: "manga", Name: "MangaFire", Type: "manga"}
	model.resolvingEpisode = &anilist.Entry{Type: "MANGA", Progress: 11}
	updated, _ := model.update(detailsMsg{media: catalog.Media{Title: "Series", Episodes: []catalog.Episode{
		{URL: "/unofficial-12", EpisodeNumber: "12", Name: "Ch. 12 - Fan translation", Scanlator: "unofficial"},
		{URL: "/group-10", EpisodeNumber: "10", Name: "Ch. 10 - Group translation", Scanlator: "Fan Group"},
		{URL: "/official-12", EpisodeNumber: "12", Name: "Ch. 12 - Publisher translation", Scanlator: "official"},
		{URL: "/official-11", EpisodeNumber: "11", Name: "Ch. 11", Scanlator: "official", Description: "{}", Memo: "{}"},
		{URL: "/unlabelled-9", EpisodeNumber: "9", Name: "Ch. 9"},
		{URL: "/unofficial-8", EpisodeNumber: "8", Name: "Ch. 8", Scanlator: "unofficial"},
	}}})
	model = updated.(Model)
	allURLs := []string{"/unofficial-12", "/group-10", "/official-12", "/official-11", "/unlabelled-9", "/unofficial-8"}
	if model.chapterScanlator != "" || model.currentLength() != len(allURLs) {
		t.Fatal("chapter list did not start with All selected")
	}
	view := ansi.Strip(model.View())
	for _, label := range []string{"All 6", "unofficial 2", "Fan Group 1", "official 2", "unofficial • Fan translation", "official • Publisher translation"} {
		if !strings.Contains(view, label) {
			t.Fatalf("chapter view missing %q:\n%s", label, view)
		}
	}
	if strings.Contains(view, "{}") {
		t.Fatalf("empty runtime metadata shown as chapter text:\n%s", view)
	}

	steps := []struct {
		key       tea.KeyMsg
		scanlator string
		urls      []string
		selected  string
	}{
		{tea.KeyMsg{Type: tea.KeyTab}, "unofficial", []string{"/unofficial-12", "/unofficial-8"}, "/unofficial-12"},
		{tea.KeyMsg{Type: tea.KeyTab}, "Fan Group", []string{"/group-10"}, "/group-10"},
		{tea.KeyMsg{Type: tea.KeyTab}, "official", []string{"/official-12", "/official-11"}, "/official-12"},
		{tea.KeyMsg{Type: tea.KeyTab}, "", allURLs, "/official-12"},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, "official", []string{"/official-12", "/official-11"}, "/official-12"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "Fan Group", []string{"/group-10"}, "/group-10"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, "unofficial", []string{"/unofficial-12", "/unofficial-8"}, "/unofficial-12"},
		{tea.KeyMsg{Type: tea.KeyRight}, "Fan Group", []string{"/group-10"}, "/group-10"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}, "official", []string{"/official-12", "/official-11"}, "/official-12"},
	}
	for index, step := range steps {
		updated, command := model.handleKey(step.key)
		model = updated.(Model)
		if command != nil {
			t.Fatalf("step %d: changing scanlator started an external request", index)
		}
		urls := make([]string, 0, len(model.episodeList.Items()))
		for _, raw := range model.episodeList.Items() {
			urls = append(urls, raw.(episodeListItem).episode.URL)
		}
		if model.chapterScanlator != step.scanlator || !slices.Equal(urls, step.urls) {
			t.Fatalf("step %d: scanlator %q chapters %v, want %q %v", index, model.chapterScanlator, urls, step.scanlator, step.urls)
		}
		selected := model.episodeList.SelectedItem().(episodeListItem)
		if selected.episode.URL != step.selected || model.cursor != model.episodeList.Index() {
			t.Fatalf("step %d: selected %q at cursor %d/list %d, want %q", index, selected.episode.URL, model.cursor, model.episodeList.Index(), step.selected)
		}
		if model.currentLength() != len(step.urls) {
			t.Fatalf("step %d: navigation length = %d, want %d", index, model.currentLength(), len(step.urls))
		}
	}
}

func TestMangaChapterFilterResetsForNewTitleOrSource(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"title", "source"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			model := New(nil, nil, "", "mpv")
			model.selectedSource = catalog.Source{ID: "first", Type: "manga"}
			model.resolvingEpisode = &anilist.Entry{Type: "MANGA"}
			media := catalog.Media{Title: "First", URL: "/first", Episodes: []catalog.Episode{
				{URL: "/official", EpisodeNumber: "1", Scanlator: "official"},
				{URL: "/unofficial", EpisodeNumber: "1", Scanlator: "unofficial"},
			}}
			updated, _ := model.update(detailsMsg{media: media})
			model = updated.(Model)
			updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
			model = updated.(Model)
			if model.chapterScanlator != "official" || model.currentLength() != 1 {
				t.Fatal("setup did not select official chapters")
			}

			if change == "title" {
				media.Title, media.URL = "Second", "/second"
			} else {
				model.selectedSource.ID = "second"
			}
			updated, _ = model.update(detailsMsg{media: media})
			model = updated.(Model)
			if model.chapterScanlator != "" || model.currentLength() != 2 {
				t.Fatalf("%s change retained filter %q with %d chapters", change, model.chapterScanlator, model.currentLength())
			}
		})
	}
}

func TestChapterScanlatorControlOnlyUsesMangaMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		sourceType   string
		chapters     []catalog.Episode
		showSelector bool
	}{
		{name: "anime", sourceType: "anime", chapters: []catalog.Episode{{URL: "/episode", Scanlator: "official"}}},
		{name: "unlabelled manga", sourceType: "manga", chapters: []catalog.Episode{{URL: "/chapter", Scanlator: " "}}},
		{name: "empty manga", sourceType: "manga"},
		{name: "single group", sourceType: "manga", chapters: []catalog.Episode{{URL: "/chapter", Scanlator: "Group A"}}, showSelector: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			model := New(nil, nil, "", "mpv")
			model.width, model.height = 100, 30
			model.loading = false
			model.stage = stageEpisodes
			model.selectedSource = catalog.Source{Type: test.sourceType}
			model.details = catalog.Media{Title: "Series", Episodes: test.chapters}
			model.setEpisodeItems()
			view := ansi.Strip(model.View())
			if shown := strings.Contains(view, "switch scanlator"); shown != test.showSelector {
				t.Fatalf("scanlator control shown = %v, want %v:\n%s", shown, test.showSelector, view)
			}
			for _, key := range []tea.KeyMsg{{Type: tea.KeyTab}, {Type: tea.KeyShiftTab}} {
				updated, command := model.handleKey(key)
				model = updated.(Model)
				if command != nil || model.currentLength() != len(test.chapters) {
					t.Fatal("scanlator navigation changed unrelated list contents")
				}
			}
		})
	}
}

func TestMangaChapterFilterPreservesReaderAndResumeVersion(t *testing.T) {
	t.Parallel()
	store := appconfig.NewStore(filepath.Join(t.TempDir(), "config.toml"))
	reader := &fakeMangaReader{events: make(chan mangareader.ProgressEvent, 1)}
	model := New(nil, store, "", "mpv", WithMangaReader(reader))
	source := catalog.Source{ID: "manga", Name: "MangaFire", Type: "manga"}
	model.homeType, model.selectedSource = "manga", source
	model.sources = []catalog.Source{source}
	model.resolvingEpisode = &anilist.Entry{ID: 1, MediaID: 125116, Type: "MANGA", Progress: 6}
	updated, _ := model.update(detailsMsg{media: catalog.Media{Title: "Wistoria", URL: "/wistoria", Episodes: []catalog.Episode{
		{URL: "/unofficial-8", EpisodeNumber: "8", Name: "Ch. 8", Scanlator: "unofficial"},
		{URL: "/official-8", EpisodeNumber: "8", Name: "Ch. 8", Scanlator: "official"},
		{URL: "/unofficial-7", EpisodeNumber: "7", Name: "Ch. 7", Scanlator: "unofficial"},
		{URL: "/official-7", EpisodeNumber: "7", Name: "Ch. 7", Scanlator: "official"},
	}}})
	model = updated.(Model)
	for range 2 {
		updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(Model)
	}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.selectedEpisode.URL != "/official-7" || model.selectedEpisode.Scanlator != "official" {
		t.Fatalf("selected the wrong chapter version: %#v", model.selectedEpisode)
	}

	pages := []catalog.Page{{URL: "one"}, {URL: "two"}, {URL: "three"}}
	updated, command = model.update(pagesMsg{items: pages})
	model = updated.(Model)
	if command == nil {
		t.Fatal("filtered chapter did not open the reader")
	}
	opened := command().(mangaReaderOpenedMsg)
	updated, _ = model.update(opened)
	model = updated.(Model)
	updated, _ = model.update(mangaReaderProgressMsg{event: mangareader.ProgressEvent{
		SessionID: opened.session.ID, Page: 2, Total: len(pages),
	}})
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	selected := model.episodeList.SelectedItem().(episodeListItem)
	if model.stage != stageEpisodes || model.chapterScanlator != "official" || selected.episode.URL != "/official-7" || model.cursor != model.episodeList.Index() {
		t.Fatalf("reader return lost the selected version: stage %d filter %q chapter %#v", model.stage, model.chapterScanlator, selected.episode)
	}
	if reader.closed != opened.session.ID {
		t.Fatal("returning to the filtered chapter list left the reader open")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.MangaReading) != 1 {
		t.Fatalf("reading history = %#v", loaded.MangaReading)
	}
	entry := loaded.MangaReading[0]
	if entry.ChapterURL != "/official-7" || entry.ChapterScanlator != "official" || entry.Page != 2 {
		t.Fatalf("persisted the wrong reading version: %#v", entry)
	}
	restartedReader := &fakeMangaReader{events: make(chan mangareader.ProgressEvent, 1)}
	restarted := New(nil, store, "", "mpv", WithMangaReader(restartedReader))
	restarted.sources = []catalog.Source{source}
	updated, command = restarted.resumeManga(entry)
	restarted = updated.(Model)
	if command == nil || restarted.selectedEpisode.URL != "/official-7" || restarted.selectedEpisode.Scanlator != "official" {
		t.Fatalf("Resume switched chapter versions: %#v", restarted.selectedEpisode)
	}
	updated, command = restarted.update(pagesMsg{items: pages})
	restarted = updated.(Model)
	if command == nil {
		t.Fatal("Resume did not open the reader")
	}
	_ = command()
	if restartedReader.session.StartPage != 2 {
		t.Fatalf("resumed page = %d, want 2", restartedReader.session.StartPage)
	}
}
