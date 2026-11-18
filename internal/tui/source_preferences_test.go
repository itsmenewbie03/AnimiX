package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/catalog"
)

func TestSourcePreferenceEditorsCancelWithoutMutation(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.stage = stageSourcePreferences
	model.loading = false
	model.sourcePreferences = []catalog.Preference{
		{Key: "quality", Kind: catalog.PreferenceList, Enabled: true, Value: "vtt", Options: []catalog.PreferenceOption{{Label: "VTT", Value: "vtt"}, {Label: "ASS", Value: "ass"}}},
		{Key: "hosts", Kind: catalog.PreferenceMultiSelect, Enabled: true, Value: []string{"a"}, Options: []catalog.PreferenceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}},
		{Key: "name", Kind: catalog.PreferenceText, Enabled: true, Value: "original"},
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model.cursor = 1
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.stage != stageSourcePreferences || model.sourcePreferences[0].Value != "vtt" {
		t.Fatalf("list cancel changed model: stage=%v value=%#v", model.stage, model.sourcePreferences[0].Value)
	}

	model.cursor = 1
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model.cursor = 1
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if !reflect.DeepEqual(model.sourcePreferences[1].Value, []string{"a"}) {
		t.Fatalf("multi-select cancel changed value: %#v", model.sourcePreferences[1].Value)
	}

	model.cursor = 2
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model.preferenceInput.SetValue("edited")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.sourcePreferences[2].Value != "original" {
		t.Fatalf("text cancel changed value: %#v", model.sourcePreferences[2].Value)
	}
}

func TestSourcePreferenceEditorsSaveThroughCommand(t *testing.T) {
	t.Parallel()
	tests := []catalog.Preference{
		{Key: "enabled", Kind: catalog.PreferenceSwitch, Enabled: true, Value: false},
		{Key: "quality", Kind: catalog.PreferenceList, Enabled: true, Value: "vtt", Options: []catalog.PreferenceOption{{Label: "VTT", Value: "vtt"}}},
		{Key: "hosts", Kind: catalog.PreferenceMultiSelect, Enabled: true, Value: []string{}, Options: []catalog.PreferenceOption{{Label: "A", Value: "a"}}},
		{Key: "name", Kind: catalog.PreferenceText, Enabled: true, Value: "value"},
	}
	for _, preference := range tests {
		preference := preference
		t.Run(string(preference.Kind), func(t *testing.T) {
			model := New(nil, nil, "", "")
			model.stage = stageSourcePreferences
			model.loading = false
			model.sourcePreferences = []catalog.Preference{preference}
			updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			model = updated.(Model)
			if preference.Kind != catalog.PreferenceSwitch {
				updated, command = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
				model = updated.(Model)
			}
			if command == nil || !model.loading {
				t.Fatalf("save did not start for %s: loading=%t command=%v", preference.Kind, model.loading, command)
			}
		})
	}
}

func TestDisabledSourcePreferenceCannotOpen(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.stage = stageSourcePreferences
	model.loading = false
	model.sourcePreferences = []catalog.Preference{{Key: "action", Kind: catalog.PreferenceUnsupported, Enabled: false}}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.stage != stageSourcePreferences || command != nil {
		t.Fatalf("unsupported preference opened: stage=%v command=%v", model.stage, command)
	}
}

func TestPreferenceCategoriesAreOmitted(t *testing.T) {
	t.Parallel()
	items := withoutPreferenceCategories([]catalog.Preference{
		{Title: "Playback", Kind: catalog.PreferenceCategory},
		{Key: "quality", Title: "Quality", Kind: catalog.PreferenceList},
	})
	if len(items) != 1 || items[0].Key != "quality" {
		t.Fatalf("visible preferences = %#v", items)
	}
}

func TestPreferenceEditorRendersAsModalOverSettings(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.width, model.height, model.loading = 100, 30, false
	model.stage = stagePreferenceList
	model.selectedSource = catalog.Source{Name: "Fixture"}
	model.sourcePreferences = []catalog.Preference{{
		Key: "mirror", Title: "Preferred Mirror", Kind: catalog.PreferenceList, Enabled: true, Value: "two",
		Options: []catalog.PreferenceOption{{Label: "Server One", Value: "one"}, {Label: "Server Two", Value: "two"}},
	}}
	view := model.View()
	for _, text := range []string{"Fixture Settings", "Preferred Mirror", "Server One", "Server Two", "esc cancel"} {
		if !strings.Contains(view, text) {
			t.Fatalf("modal view does not contain %q:\n%s", text, view)
		}
	}
}

func TestTextPreferenceStaysFocusedWhileSaveIsPending(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.loading = false
	model.stage, model.preferenceIndex = stagePreferenceText, 0
	model.sourcePreferences = []catalog.Preference{{Key: "token", Kind: catalog.PreferenceText, Enabled: true}}
	model.preferenceInput.Focus()
	updated, command := model.handleSourcePreferenceKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.loading || !model.preferenceInput.Focused() || command == nil {
		t.Fatalf("pending text save: loading=%t focused=%t command=%v", model.loading, model.preferenceInput.Focused(), command)
	}
}

func TestExtensionPreferenceBreadcrumbUsesExtensionParent(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.stage = stageSourcePreferences
	model.sourcePreferencesReturn = stageExtensions
	model.selectedSource = catalog.Source{Name: "Fixture"}
	if got := model.breadcrumb(); got != "Home › Extensions › Fixture › Source settings" {
		t.Fatalf("breadcrumb = %q", got)
	}
}

func TestPreferenceModalKeepsItsHeightNearEndOfOptions(t *testing.T) {
	t.Parallel()
	model := New(nil, nil, "", "")
	model.width, model.height, model.loading = 100, 30, false
	model.stage, model.preferenceIndex = stagePreferenceList, 0
	model.sourcePreferences = []catalog.Preference{{
		Key: "mirror", Title: "Preferred Mirror", Kind: catalog.PreferenceList, Enabled: true,
		Options: []catalog.PreferenceOption{
			{Label: "One", Value: "one"},
			{Label: "Two", Value: "two"},
			{Label: "Three", Value: "three"},
			{Label: "Four", Value: "four"},
			{Label: "Five", Value: "five"},
		},
	}}
	model.cursor = 0
	firstHeight := strings.Count(model.preferenceModalContent(), "\n")
	model.cursor = 4
	last := model.preferenceModalContent()
	lastHeight := strings.Count(last, "\n")
	if lastHeight != firstHeight {
		t.Fatalf("modal shrank from %d to %d lines near the final option:\n%s", firstHeight, lastHeight, last)
	}
	for _, label := range []string{"One", "Two", "Three", "Four", "Five"} {
		if !strings.Contains(last, label) {
			t.Fatalf("final modal viewport lost %q:\n%s", label, last)
		}
	}
}

func TestUnsetListPreferenceSelectsFirstOptionAsEffectiveDefault(t *testing.T) {
	t.Parallel()
	preference := catalog.Preference{
		Key: "mirror", Title: "Preferred Mirror", Kind: catalog.PreferenceList, Enabled: true,
		Options: []catalog.PreferenceOption{{Label: "Default Mirror", Value: "default"}, {Label: "Backup", Value: "backup"}},
	}
	if got := preferenceSecondary(preference); got != "Default Mirror" {
		t.Fatalf("unset preference subtitle = %q", got)
	}
	model := New(nil, nil, "", "")
	model.width, model.height, model.loading = 100, 30, false
	model.stage, model.preferenceIndex, model.cursor = stagePreferenceList, 0, preferenceOptionIndex(preference)
	model.sourcePreferences = []catalog.Preference{preference}
	content := model.preferenceModalContent()
	if !strings.Contains(content, "(●) Default Mirror") {
		t.Fatalf("effective default is not selected:\n%s", content)
	}
}
