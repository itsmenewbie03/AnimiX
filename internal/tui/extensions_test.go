package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/repository"
	appupdate "github.com/itsmenewbie03/animix/internal/update"
)

func TestExtensionVersionNewer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		installed, available string
		want                 bool
	}{
		{"1.2.3", "1.2.4", true},
		{"v2.0", "2.0.0", false},
		{"2.1", "2.0.9", false},
		{"", "2.0", false},
	}
	for _, test := range tests {
		if got := extensionVersionNewer(test.installed, test.available); got != test.want {
			t.Errorf("extensionVersionNewer(%q, %q) = %t, want %t", test.installed, test.available, got, test.want)
		}
	}
}

func TestExtensionInventoryMatchesInstalledPackage(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.extensionType = "anime"
	model.sources = []catalog.Source{{ID: "1", Name: "Anikoto", Type: "anime", Package: "ext.anikoto", Version: "1.0"}}
	model.extensionCatalog = []repository.Extension{
		{Name: "Anikoto", Type: "anime", Package: "ext.anikoto", Version: "1.1"},
		{Name: "Other", Type: "anime", Package: "ext.other", Version: "1.0"},
	}
	if !model.sourceHasUpdate(model.sources[0]) {
		t.Fatal("newer package was not recognized as an update")
	}
	available := model.availableExtensionEntries()
	if len(available) != 1 || available[0].Package != "ext.other" {
		t.Fatalf("available extensions = %#v", available)
	}
}

func TestDeduplicateExtensionsKeepsNewestVersion(t *testing.T) {
	t.Parallel()
	items := deduplicateExtensions([]repository.Extension{
		{Name: "Fixture", Type: "anime", Package: "fixture", Version: "1.0"},
		{Name: "Fixture", Type: "anime", Package: "fixture", Version: "1.2"},
	})
	if len(items) != 1 || items[0].Version != "1.2" {
		t.Fatalf("deduplicated extensions = %#v", items)
	}
}

func TestHomeExtensionsShortcutIsVisibleWorkflow(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.loading = false
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(Model)
	if model.stage != stageExtensions || !model.loading || command == nil {
		t.Fatalf("extensions shortcut: stage=%v loading=%t command=%v", model.stage, model.loading, command)
	}
}

func TestExtensionUninstallRequiresConfirmation(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.loading = false
	model.stage, model.extensionType, model.extensionsInstalled = stageExtensions, "anime", true
	model.sources = []catalog.Source{{ID: "1", Name: "Fixture", Type: "anime", Package: "ext.fixture"}}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(Model)
	if model.stage != stageExtensionRemove || command != nil || model.pendingExtension.Package != "ext.fixture" {
		t.Fatalf("uninstall did not open confirmation: stage=%v pending=%#v command=%v", model.stage, model.pendingExtension, command)
	}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).stage != stageExtensions {
		t.Fatal("cancel did not return to extensions")
	}
}

func TestExtensionUpdateKeyWinsOverApplicationUpdateShortcut(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.loading = false
	model.stage, model.extensionType, model.extensionsInstalled = stageExtensions, "anime", true
	model.availableUpdate = appupdate.Release{Version: "v99"}
	model.sources = []catalog.Source{{ID: "1", Name: "Fixture", Type: "anime", Package: "ext.fixture", Version: "1.0"}}
	model.extensionCatalog = []repository.Extension{{Name: "Fixture", Type: "anime", Package: "ext.fixture", Version: "1.1"}}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	model = updated.(Model)
	if model.stage != stageExtensions || !model.loading || command == nil {
		t.Fatalf("extension update was intercepted: stage=%v loading=%t command=%v", model.stage, model.loading, command)
	}
}

func TestExtensionNavigationAndLanguageKeysDoNotOverlap(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.loading = false
	model.stage, model.extensionType, model.extensionsInstalled = stageExtensions, "anime", true
	model.extensionCatalog = []repository.Extension{{Name: "Fixture", Type: "anime", Language: "en"}}

	updated, _ := model.handleExtensionsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = updated.(Model)
	if model.extensionsInstalled {
		t.Fatal("l did not switch to the Available view")
	}

	updated, _ = model.handleExtensionsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	model = updated.(Model)
	if model.extensionLanguage != "EN" || model.extensionsInstalled {
		t.Fatalf("L filter result: language=%q installed=%t", model.extensionLanguage, model.extensionsInstalled)
	}
}

func TestAvailableExtensionsKeepScreenChromeWithinViewport(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.width, model.height, model.loading = 100, 30, false
	model.stage, model.extensionType, model.extensionsInstalled = stageExtensions, "anime", false
	for index := 0; index < 100; index++ {
		model.extensionCatalog = append(model.extensionCatalog, repository.Extension{
			Name: fmt.Sprintf("Extension %03d", index), Type: "anime", Language: "en", Package: fmt.Sprintf("ext.%03d", index),
		})
	}
	view := model.View()
	for _, header := range []string{"AnimiX", "Home › Extensions", "Extensions", "Available 100"} {
		if !strings.Contains(view, header) {
			t.Fatalf("available view lost header %q:\n%s", header, view)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines > model.height {
		t.Fatalf("available view uses %d lines in a %d-line viewport", lines, model.height)
	}
}

func TestInstallingExtensionKeepsInventoryAndShowsRowProgress(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.width, model.height = 100, 30
	model.loading = true
	model.stage, model.extensionType, model.extensionsInstalled = stageExtensions, "anime", false
	model.extensionCatalog = []repository.Extension{{Name: "Fixture", Type: "anime", Language: "en", Package: "ext.fixture"}}
	view := model.extensionsView()
	if strings.Contains(view, "Refreshing extensions") || !strings.Contains(view, "Fixture") {
		t.Fatalf("install replaced the selected row with a refresh screen:\n%s", view)
	}
}

func TestExtensionCatalogKeepsResultsFromSuccessfulRepositories(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/broken") {
			http.Error(response, "broken", http.StatusBadGateway)
			return
		}
		_, _ = response.Write([]byte(`[{"name":"Aniyomi: Fixture","pkg":"ext.fixture","apk":"fixture.apk","version":"1.0","sources":[{"id":"1","lang":"en"}]}]`))
	}))
	defer server.Close()

	model := New(nil, nil, "", "")
	model.repositoryClient = repository.NewClient()
	model.repositories = []appconfig.Repository{
		{URL: server.URL + "/working", Type: "anime"},
		{URL: server.URL + "/broken", Type: "anime"},
	}
	message := model.loadExtensionCatalog()().(extensionsMsg)
	if message.err != nil || len(message.items) != 1 || message.warning == "" {
		t.Fatalf("catalog result = items %#v, warning %q, error %v", message.items, message.warning, message.err)
	}
}
