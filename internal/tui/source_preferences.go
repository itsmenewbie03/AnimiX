package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/itsmenewbie03/animix/internal/catalog"
)

func (m Model) isSourcePreferenceStage() bool {
	switch m.stage {
	case stageSourcePreferences, stagePreferenceList, stagePreferenceMultiSelect, stagePreferenceText:
		return true
	default:
		return false
	}
}

func (m Model) loadSourcePreferences() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.requestTimeout())
		defer cancel()
		items, err := m.service.SourcePreferences(ctx, m.selectedSource)
		items = withoutPreferenceCategories(items)
		return sourcePreferencesMsg{items: items, cursor: 0, err: err}
	}
}

func (m Model) saveSourcePreference(key string, value any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.requestTimeout())
		defer cancel()
		items, err := m.service.UpdateSourcePreference(ctx, m.selectedSource, key, value)
		items = withoutPreferenceCategories(items)
		return sourcePreferencesMsg{items: items, cursor: m.preferenceIndex, err: err}
	}
}

func withoutPreferenceCategories(items []catalog.Preference) []catalog.Preference {
	result := make([]catalog.Preference, 0, len(items))
	for _, item := range items {
		if item.Kind != catalog.PreferenceCategory {
			result = append(result, item)
		}
	}
	return result
}

func (m Model) selectedPreference() (catalog.Preference, bool) {
	if m.preferenceIndex < 0 || m.preferenceIndex >= len(m.sourcePreferences) {
		return catalog.Preference{}, false
	}
	return m.sourcePreferences[m.preferenceIndex], true
}

func (m Model) handleSourcePreferenceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "q" && m.stage != stagePreferenceText {
		return m, tea.Quit
	}
	switch m.stage {
	case stageSourcePreferences:
		switch key.String() {
		case "esc", "backspace":
			if m.sourcePreferencesReturn == stageExtensions {
				m.stage, m.cursor = stageExtensions, 0
				for index, source := range m.installedExtensionSources() {
					if source.ID == m.selectedSource.ID {
						m.cursor = index
						break
					}
				}
			} else {
				m.stage, m.cursor = stageDiscoverSources, m.discoverSourceCursor
			}
			return m, nil
		case "up", "k":
			m.cursor = m.nextPreferenceCursor(-1)
			return m, nil
		case "down", "j":
			m.cursor = m.nextPreferenceCursor(1)
			return m, nil
		case " ", "space":
			if len(m.sourcePreferences) > 0 && m.sourcePreferences[m.cursor].Kind == catalog.PreferenceSwitch && m.sourcePreferences[m.cursor].Editable() {
				preference := m.sourcePreferences[m.cursor]
				value, _ := preference.Value.(bool)
				m.preferenceIndex, m.loading = m.cursor, true
				return m, tea.Batch(m.spinner.Tick, m.saveSourcePreference(preference.Key, !value))
			}
		case "enter":
			if len(m.sourcePreferences) == 0 {
				return m, nil
			}
			preference := m.sourcePreferences[m.cursor]
			if !preference.Editable() {
				return m, nil
			}
			m.preferenceIndex = m.cursor
			switch preference.Kind {
			case catalog.PreferenceSwitch:
				value, _ := preference.Value.(bool)
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.saveSourcePreference(preference.Key, !value))
			case catalog.PreferenceList:
				m.stage, m.cursor = stagePreferenceList, preferenceOptionIndex(preference)
			case catalog.PreferenceMultiSelect:
				m.stage, m.cursor = stagePreferenceMultiSelect, 0
				m.preferenceSelections = make(map[string]bool)
				for _, value := range preferenceStringSlice(preference.Value) {
					m.preferenceSelections[value] = true
				}
			case catalog.PreferenceText:
				m.stage = stagePreferenceText
				m.preferenceInput.SetValue(preferenceString(preference.Value))
				m.preferenceInput.Focus()
				return m, textinput.Blink
			}
			return m, nil
		}
	case stagePreferenceList:
		preference, ok := m.selectedPreference()
		if !ok {
			m.stage, m.cursor = stageSourcePreferences, 0
			return m, nil
		}
		switch key.String() {
		case "esc", "backspace":
			m.stage, m.cursor = stageSourcePreferences, m.preferenceIndex
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(preference.Options)-1), m.cursor+1)
		case "enter":
			if len(preference.Options) > 0 {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.saveSourcePreference(preference.Key, preference.Options[m.cursor].Value))
			}
		}
		return m, nil
	case stagePreferenceMultiSelect:
		preference, ok := m.selectedPreference()
		if !ok {
			m.stage, m.cursor = stageSourcePreferences, 0
			return m, nil
		}
		switch key.String() {
		case "esc", "backspace":
			m.stage, m.cursor = stageSourcePreferences, m.preferenceIndex
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(preference.Options)-1), m.cursor+1)
		case " ", "space":
			if len(preference.Options) > 0 {
				value := preference.Options[m.cursor].Value
				m.preferenceSelections[value] = !m.preferenceSelections[value]
			}
		case "enter":
			values := make([]string, 0, len(preference.Options))
			for _, option := range preference.Options {
				if m.preferenceSelections[option.Value] {
					values = append(values, option.Value)
				}
			}
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.saveSourcePreference(preference.Key, values))
		}
		return m, nil
	case stagePreferenceText:
		preference, ok := m.selectedPreference()
		if !ok {
			m.stage, m.cursor = stageSourcePreferences, 0
			return m, nil
		}
		switch key.String() {
		case "esc":
			m.preferenceInput.Blur()
			m.stage, m.cursor = stageSourcePreferences, m.preferenceIndex
			return m, nil
		case "enter":
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.saveSourcePreference(preference.Key, m.preferenceInput.Value()))
		default:
			var cmd tea.Cmd
			m.preferenceInput, cmd = m.preferenceInput.Update(key)
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) nextPreferenceCursor(delta int) int {
	if len(m.sourcePreferences) == 0 {
		return 0
	}
	index := m.cursor
	for attempts := 0; attempts < len(m.sourcePreferences); attempts++ {
		index = max(0, min(len(m.sourcePreferences)-1, index+delta))
		if m.sourcePreferences[index].Editable() {
			return index
		}
		if index == 0 || index == len(m.sourcePreferences)-1 {
			break
		}
	}
	return m.cursor
}

func firstEditablePreference(items []catalog.Preference, fallback int) int {
	for index, item := range items {
		if item.Editable() {
			return index
		}
	}
	return fallback
}

func preferenceOptionIndex(preference catalog.Preference) int {
	value := preferenceString(preference.Value)
	for index, option := range preference.Options {
		if option.Value == value {
			return index
		}
	}
	return 0
}

func effectivePreferenceOption(preference catalog.Preference) (catalog.PreferenceOption, bool) {
	value := preferenceString(preference.Value)
	for _, option := range preference.Options {
		if option.Value == value && value != "" {
			return option, true
		}
	}
	if len(preference.Options) > 0 {
		return preference.Options[0], true
	}
	return catalog.PreferenceOption{}, false
}

func preferenceString(value any) string {
	text, _ := value.(string)
	return text
}

func preferenceStringSlice(value any) []string {
	values, _ := value.([]string)
	return values
}

func (m Model) sourcePreferencesView() string {
	if m.loading && len(m.sourcePreferences) == 0 {
		return m.spinner.View() + " Loading source settings…"
	}
	if len(m.sourcePreferences) == 0 {
		returnLabel := "discover"
		if m.sourcePreferencesReturn == stageExtensions {
			returnLabel = "extensions"
		}
		return titleStyle.Render("Source settings") + "\n\n" +
			helpStyle.Render("This source has no configurable settings") + "\n\n" +
			helpStyle.Render("esc "+returnLabel)
	}
	var output strings.Builder
	output.WriteString(titleStyle.Render(m.selectedSource.Name+" Settings") + "\n\n")
	available := max(2, (m.height-10)/2)
	start := max(0, m.cursor-available/2)
	end := min(len(m.sourcePreferences), start+available)
	if end-start < available {
		start = max(0, end-available)
	}
	rowWidth := max(24, m.width-10)
	for index := start; index < end; index++ {
		preference := m.sourcePreferences[index]
		focused := index == m.cursor
		selector := "  "
		if focused {
			selector = "┃ "
			if m.loading {
				selector = ansi.Strip(m.spinner.View()) + " "
			}
		}
		title := preferenceTitle(preference)
		status := preferenceStatus(preference)
		if status != "" {
			space := max(1, rowWidth-lipgloss.Width(selector)-lipgloss.Width(status)-lipgloss.Width(title))
			title += strings.Repeat(" ", space) + status
		}
		title = ansi.Truncate(title, rowWidth-lipgloss.Width(selector), "…")
		primary := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E6F0"))
		if focused {
			primary = primary.Foreground(accent).Bold(true)
		}
		if !preference.Editable() {
			primary = helpStyle
		}
		output.WriteString(lipgloss.NewStyle().Foreground(accent).Render(selector) + primary.Render(title) + "\n")
		secondary := ansi.Truncate(preferenceSecondary(preference), max(12, rowWidth-4), "…")
		output.WriteString("    " + helpStyle.Render(secondary) + "\n")
	}
	help := "enter edit • space toggle • esc back"
	if m.loading {
		help = "saving…"
	}
	return output.String() + "\n" + helpStyle.Render(help)
}

func preferenceTitle(preference catalog.Preference) string {
	if title := strings.TrimSpace(preference.Title); title != "" {
		return title
	}
	key := strings.NewReplacer("_", " ", "-", " ").Replace(preference.Key)
	words := strings.Fields(key)
	for i := range words {
		if len(words[i]) > 0 {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return firstNonEmpty(strings.Join(words, " "), "Untitled setting")
}

func preferenceStatus(preference catalog.Preference) string {
	if preference.Kind != catalog.PreferenceSwitch {
		return ""
	}
	if value, _ := preference.Value.(bool); value {
		return "ON"
	}
	return "OFF"
}

func preferenceSecondary(preference catalog.Preference) string {
	switch preference.Kind {
	case catalog.PreferenceList:
		if option, ok := effectivePreferenceOption(preference); ok {
			return option.Label
		}
		return "Select an option"
	case catalog.PreferenceMultiSelect:
		labels := make([]string, 0)
		for _, value := range preferenceStringSlice(preference.Value) {
			labels = append(labels, preferenceOptionLabel(preference, value))
		}
		if len(labels) == 0 {
			return "Nothing selected"
		}
		if len(labels) > 3 {
			return fmt.Sprintf("%d selected", len(labels))
		}
		return strings.Join(labels, ", ")
	case catalog.PreferenceText:
		return firstNonEmpty(preferenceString(preference.Value), "Not set")
	case catalog.PreferenceUnsupported:
		return "Unavailable in AnimiX"
	default:
		return firstNonEmpty(preference.Summary, "Toggle setting")
	}
}

func (m Model) preferenceModalContent() string {
	preference, ok := m.selectedPreference()
	if !ok {
		return helpStyle.Render("Preference is no longer available")
	}
	title := titleStyle.Render(firstNonEmpty(preference.Title, preference.Key))
	summary := ""
	if strings.TrimSpace(preference.Summary) != "" {
		summary = "\n" + helpStyle.Render(preference.Summary)
	}
	switch m.stage {
	case stagePreferenceText:
		content := title + summary + "\n\n" + m.preferenceInput.View()
		if m.loading {
			content += "\n\n" + m.spinner.View() + " Saving…"
		} else {
			content += "\n\n" + helpStyle.Render("enter save • esc cancel")
		}
		return content
	case stagePreferenceList, stagePreferenceMultiSelect:
		var list strings.Builder
		available := max(3, min(len(preference.Options), m.height-12))
		start := max(0, m.cursor-available/2)
		end := min(len(preference.Options), start+available)
		if end-start < available {
			start = max(0, end-available)
		}
		for index := start; index < end; index++ {
			option := preference.Options[index]
			mark := "( )"
			if m.stage == stagePreferenceMultiSelect {
				mark = "[ ]"
				if m.preferenceSelections[option.Value] {
					mark = "[✓]"
				}
			} else if index == m.cursor {
				mark = "(●)"
			}
			selector := "  "
			if index == m.cursor {
				selector = "┃ "
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E6F0"))
			if index == m.cursor {
				style = style.Foreground(accent).Bold(true)
			}
			list.WriteString(lipgloss.NewStyle().Foreground(accent).Render(selector) + style.Render(mark+" "+option.Label) + "\n")
		}
		help := "enter save • esc cancel"
		if m.stage == stagePreferenceMultiSelect {
			help = "space toggle • enter apply • esc cancel"
		}
		if m.loading {
			help = m.spinner.View() + " Saving…"
		}
		return title + summary + "\n\n" + list.String() + "\n" + helpStyle.Render(help)
	}
	return ""
}

func (m Model) preferenceModalView(base string) string {
	viewportWidth := max(40, m.width)
	modalWidth := min(68, max(24, viewportWidth-8))
	panel := lipgloss.NewStyle().Width(modalWidth).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Render(m.preferenceModalContent())
	return overlayCentered(base, panel, max(0, (m.height-lipgloss.Height(panel))/2), viewportWidth)
}

func overlayCentered(base, popup string, row, width int) string {
	baseLines, popupLines := strings.Split(base, "\n"), strings.Split(popup, "\n")
	popupWidth := lipgloss.Width(popup)
	left := max(0, (width-popupWidth)/2)
	for len(baseLines) < row+len(popupLines) {
		baseLines = append(baseLines, "")
	}
	for index, popupLine := range popupLines {
		lineIndex := row + index
		line := baseLines[lineIndex]
		prefix := ansi.Cut(line, 0, left)
		prefix += strings.Repeat(" ", max(0, left-ansi.StringWidth(prefix)))
		suffix := ansi.Cut(line, left+popupWidth, width)
		baseLines[lineIndex] = prefix + popupLine + suffix
	}
	return strings.Join(baseLines, "\n")
}

func preferenceOptionLabel(preference catalog.Preference, value string) string {
	for _, option := range preference.Options {
		if option.Value == value {
			return option.Label
		}
	}
	return value
}
