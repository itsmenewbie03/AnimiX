package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/repository"
)

type extensionBatchMsg struct{ err error }

func (m Model) loadExtensionCatalog() tea.Cmd {
	repositories := append([]appRepository(nil), repositoriesForExtensions(m.repositories)...)
	return func() tea.Msg {
		items := make([]repository.Extension, 0)
		var failures []string
		for _, repo := range repositories {
			entries, err := m.repositoryClient.Fetch(repo.URL, repo.Type)
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			items = append(items, entries...)
		}
		items = deduplicateExtensions(items)
		if len(items) == 0 && len(failures) > 0 {
			return extensionsMsg{err: fmt.Errorf("refresh extensions: %s", strings.Join(failures, "; "))}
		}
		message := ""
		if len(failures) > 0 {
			message = fmt.Sprintf("Loaded available extensions, but %d repositories failed: %s", len(failures), strings.Join(failures, "; "))
		}
		return extensionsMsg{items: items, warning: message}
	}
}

// Keep the command closure independent of mutable model configuration.
type appRepository struct{ URL, Type string }

func repositoriesForExtensions(items []appconfig.Repository) []appRepository {
	result := make([]appRepository, len(items))
	for i, item := range items {
		result[i] = appRepository{URL: item.URL, Type: item.Type}
	}
	return result
}

func deduplicateExtensions(items []repository.Extension) []repository.Extension {
	seen := make(map[string]int)
	result := make([]repository.Extension, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item.Type + "\x00" + firstNonEmpty(item.Package, item.Name))
		if index, ok := seen[key]; ok {
			if extensionVersionNewer(result[index].Version, item.Version) {
				result[index] = item
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func (m Model) installedExtensionSources() []catalog.Source {
	query := strings.ToLower(strings.TrimSpace(m.extensionQueryValue()))
	result := make([]catalog.Source, 0)
	for _, source := range m.sources {
		if !strings.EqualFold(source.Type, m.extensionType) || !m.extensionLanguageMatches(source.Language) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(source.Name+" "+source.Package), query) {
			continue
		}
		result = append(result, source)
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result
}

func (m Model) availableExtensionEntries() []repository.Extension {
	query := strings.ToLower(strings.TrimSpace(m.extensionQueryValue()))
	result := make([]repository.Extension, 0)
	for _, extension := range m.extensionCatalog {
		if !strings.EqualFold(extension.Type, m.extensionType) || !m.extensionLanguageMatches(extension.Language) || m.extensionInstalled(extension) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(extension.Name+" "+extension.Package), query) {
			continue
		}
		result = append(result, extension)
	}
	return result
}

func (m Model) extensionQueryValue() string {
	if m.stage == stageExtensionsSearch {
		return m.extensionSearch.Value()
	}
	return m.extensionQuery
}

func (m Model) extensionLanguageMatches(language string) bool {
	return m.extensionLanguage == "" || strings.EqualFold(language, m.extensionLanguage)
}

func (m Model) extensionInstalled(extension repository.Extension) bool {
	for _, source := range m.sources {
		if extensionMatchesSource(extension, source) {
			return true
		}
	}
	return false
}

func extensionMatchesSource(extension repository.Extension, source catalog.Source) bool {
	if !strings.EqualFold(extension.Type, source.Type) {
		return false
	}
	if extension.Package != "" && source.Package != "" {
		return extension.Package == source.Package
	}
	for _, id := range extension.SourceIDs {
		if id == source.ID {
			return true
		}
	}
	return false
}

func (m Model) availableForSource(source catalog.Source) (repository.Extension, bool) {
	for _, extension := range m.extensionCatalog {
		if extensionMatchesSource(extension, source) {
			return extension, true
		}
	}
	return repository.Extension{}, false
}

func (m Model) sourceHasUpdate(source catalog.Source) bool {
	available, ok := m.availableForSource(source)
	return ok && extensionVersionNewer(source.Version, available.Version)
}

func extensionVersionNewer(installed, available string) bool {
	left, right := versionParts(installed), versionParts(available)
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	length := max(len(left), len(right))
	for i := 0; i < length; i++ {
		var a, b int
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		if b != a {
			return b > a
		}
	}
	return false
}

func versionParts(value string) []int {
	fields := strings.FieldsFunc(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v")), func(r rune) bool { return !unicode.IsDigit(r) })
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		if number, err := strconv.Atoi(field); err == nil {
			parts = append(parts, number)
		}
	}
	return parts
}

func (m *Model) cycleManagedExtensionLanguage() {
	seen := map[string]bool{}
	languages := []string{""}
	for _, source := range m.sources {
		if strings.EqualFold(source.Type, m.extensionType) {
			seen[strings.ToUpper(source.Language)] = true
		}
	}
	for _, extension := range m.extensionCatalog {
		if strings.EqualFold(extension.Type, m.extensionType) {
			seen[strings.ToUpper(extension.Language)] = true
		}
	}
	for language := range seen {
		if language != "" {
			languages = append(languages, language)
		}
	}
	sort.Strings(languages[1:])
	index := 0
	for i, language := range languages {
		if strings.EqualFold(language, m.extensionLanguage) {
			index = i
			break
		}
	}
	m.extensionLanguage = languages[(index+1)%len(languages)]
}

func (m Model) handleExtensionsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.stage == stageExtensionRemove {
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "esc", "backspace", "n":
			m.stage = stageExtensions
			return m, nil
		case "enter", "y":
			m.stage, m.loading = stageExtensions, true
			return m, tea.Batch(m.spinner.Tick, m.uninstallExtension(m.pendingExtension))
		}
		return m, nil
	}
	if m.stage == stageExtensionsSearch {
		switch key.String() {
		case "esc":
			m.extensionSearch.Blur()
			m.extensionSearch.SetValue(m.extensionQuery)
			m.stage = stageExtensions
		case "enter":
			m.extensionQuery = strings.TrimSpace(m.extensionSearch.Value())
			m.extensionSearch.Blur()
			m.stage, m.cursor = stageExtensions, 0
		default:
			var cmd tea.Cmd
			m.extensionSearch, cmd = m.extensionSearch.Update(key)
			m.cursor = 0
			return m, cmd
		}
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.stage, m.cursor = stageSources, 0
	case "1", "2":
		if key.String() == "1" {
			m.extensionType = "anime"
		} else {
			m.extensionType = "manga"
		}
		m.extensionLanguage, m.cursor = "", 0
	case "tab", "shift+tab", "left", "right", "h", "l":
		m.extensionsInstalled = !m.extensionsInstalled
		m.cursor = 0
	case "L":
		m.cycleManagedExtensionLanguage()
		m.cursor = 0
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(0, m.currentLength()-1), m.cursor+1)
	case "/":
		m.stage = stageExtensionsSearch
		m.extensionSearch.SetValue(m.extensionQuery)
		m.extensionSearch.Focus()
		return m, textinput.Blink
	case "c":
		m.extensionQuery, m.extensionLanguage, m.cursor = "", "", 0
	case "r":
		m.repositoriesReturn = stageExtensions
		m.stage, m.cursor = stageRepositories, 0
	case "f5":
		m.loading, m.extensionRefreshing = true, true
		return m, tea.Batch(m.spinner.Tick, m.loadExtensionCatalog())
	case "enter":
		if m.extensionsInstalled {
			sources := m.installedExtensionSources()
			if len(sources) == 0 {
				return m, nil
			}
			m.selectedSource, m.preferenceIndex = sources[m.cursor], 0
			m.sourcePreferences, m.loading = nil, true
			m.stage = stageSourcePreferences
			m.sourcePreferencesReturn = stageExtensions
			return m, tea.Batch(m.spinner.Tick, m.loadSourcePreferences())
		}
		items := m.availableExtensionEntries()
		if len(items) > 0 {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.installExtension(items[m.cursor]))
		}
	case "u":
		if m.extensionsInstalled {
			sources := m.installedExtensionSources()
			if len(sources) > 0 {
				if extension, ok := m.availableForSource(sources[m.cursor]); ok && m.sourceHasUpdate(sources[m.cursor]) {
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.updateExtension(extension))
				}
			}
		}
	case "U":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, m.updateAllExtensions())
	case "x":
		if m.extensionsInstalled {
			sources := m.installedExtensionSources()
			if len(sources) > 0 {
				extension, ok := m.availableForSource(sources[m.cursor])
				if !ok {
					extension = repository.Extension{Name: sources[m.cursor].Name, Package: sources[m.cursor].Package, Type: sources[m.cursor].Type, SourceIDs: []string{sources[m.cursor].ID}}
				}
				m.pendingExtension = extension
				m.stage = stageExtensionRemove
				return m, nil
			}
		}
	}
	return m, nil
}

func (m Model) extensionRemoveModalView(base string) string {
	name := firstNonEmpty(m.pendingExtension.Name, m.pendingExtension.Package, "this extension")
	content := titleStyle.Render("Uninstall extension?") + "\n\n" +
		"Remove " + name + " and all sources provided by it?\n\n" +
		helpStyle.Render("enter/y uninstall • esc/n cancel")
	viewportWidth := max(40, m.width)
	width := min(62, max(24, viewportWidth-8))
	panel := lipgloss.NewStyle().Width(width).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#F38BA8")).Padding(1, 2).Render(content)
	return overlayCentered(base, panel, max(0, (m.height-lipgloss.Height(panel))/2), viewportWidth)
}

func (m Model) updateAllExtensions() tea.Cmd {
	updates := make(map[string]repository.Extension)
	for _, source := range m.sources {
		if !strings.EqualFold(source.Type, m.extensionType) || !m.sourceHasUpdate(source) {
			continue
		}
		if extension, ok := m.availableForSource(source); ok {
			updates[firstNonEmpty(extension.Package, extension.Name)] = extension
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for _, extension := range updates {
			for _, source := range m.sources {
				if extensionMatchesSource(extension, source) {
					if err := m.service.UnloadExtension(ctx, source.ID); err != nil {
						return extensionBatchMsg{err: fmt.Errorf("unload %s: %w", source.Name, err)}
					}
				}
			}
			if _, err := m.installer.Install(ctx, extension, m.extensionDirectory); err != nil {
				return extensionBatchMsg{err: err}
			}
		}
		return extensionBatchMsg{}
	}
}

func (m Model) updateExtension(extension repository.Extension) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for _, source := range m.sources {
			if !extensionMatchesSource(extension, source) {
				continue
			}
			if err := m.service.UnloadExtension(ctx, source.ID); err != nil {
				return extensionInstalledMsg{err: fmt.Errorf("unload %s: %w", source.Name, err)}
			}
		}
		path, err := m.installer.Install(ctx, extension, m.extensionDirectory)
		return extensionInstalledMsg{path: path, err: err}
	}
}

func (m Model) extensionsView() string {
	var output strings.Builder
	output.WriteString(titleStyle.Render("Extensions") + "\n\n")
	anime, manga := helpStyle.Render(" 1 Anime "), helpStyle.Render(" 2 Manga ")
	if m.extensionType == "anime" {
		anime = selected.Render("1 Anime")
	} else {
		manga = selected.Render("2 Manga")
	}
	output.WriteString(anime + "  " + manga + "\n\n")
	installedCount := 0
	for _, source := range m.sources {
		if strings.EqualFold(source.Type, m.extensionType) {
			installedCount++
		}
	}
	availableCount := 0
	for _, extension := range m.extensionCatalog {
		if strings.EqualFold(extension.Type, m.extensionType) && !m.extensionInstalled(extension) {
			availableCount++
		}
	}
	installedTab := homeFeedTab("Installed", installedCount, m.extensionsInstalled)
	availableTab := homeFeedTab("Available", availableCount, !m.extensionsInstalled)
	output.WriteString(installedTab + "  " + availableTab + "\n")
	output.WriteString(helpStyle.Render("tab/shift+tab or ←/→ switch view") + "\n\n")
	if m.stage == stageExtensionsSearch {
		output.WriteString("Search: " + m.extensionSearch.View() + "\n\n")
	} else if m.extensionQuery != "" || m.extensionLanguage != "" {
		query := firstNonEmpty(m.extensionQuery, "—")
		language := firstNonEmpty(strings.ToUpper(m.extensionLanguage), "ALL")
		output.WriteString(helpStyle.Render(fmt.Sprintf("Search: %s  •  Language: %s  •  c clear", query, language)) + "\n\n")
	}
	if m.extensionRefreshing {
		output.WriteString(m.spinner.View() + " Refreshing extensions…")
		return output.String()
	}
	if m.extensionsInstalled {
		sources := m.installedExtensionSources()
		items := make([]string, len(sources))
		updates := 0
		for index, source := range sources {
			version := strings.TrimPrefix(source.Version, "v")
			meta := strings.ToUpper(source.Language)
			if version != "" {
				meta += " • v" + version
			}
			if available, ok := m.availableForSource(source); ok && m.sourceHasUpdate(source) {
				meta += " → v" + strings.TrimPrefix(available.Version, "v") + " • UPDATE"
				updates++
			}
			items[index] = source.Name + "  " + meta
		}
		extraRows := 0
		if m.stage == stageExtensionsSearch || m.extensionQuery != "" || m.extensionLanguage != "" {
			extraRows += 2
		}
		if updates > 0 {
			output.WriteString(titleStyle.Render(fmt.Sprintf("Updates pending: %d", updates)) + "\n\n")
			extraRows += 2
		}
		if len(items) == 0 {
			output.WriteString(helpStyle.Render("No installed " + m.extensionType + " extensions."))
		} else {
			output.WriteString(m.listViewRows(items, m.extensionListRows(extraRows)))
		}
		output.WriteString("\n" + helpStyle.Render("enter settings • u update • U update all • x uninstall"))
	} else {
		entries := m.availableExtensionEntries()
		items := make([]string, len(entries))
		for index, extension := range entries {
			items[index] = fmt.Sprintf("%s  %s • v%s", extension.Name, strings.ToUpper(extension.Language), strings.TrimPrefix(extension.Version, "v"))
		}
		if len(items) == 0 {
			output.WriteString(helpStyle.Render("No available extensions match these filters."))
		} else {
			extraRows := 0
			if m.stage == stageExtensionsSearch || m.extensionQuery != "" || m.extensionLanguage != "" {
				extraRows = 2
			}
			output.WriteString(m.listViewRows(items, m.extensionListRows(extraRows)))
		}
		output.WriteString("\n" + helpStyle.Render("enter install"))
	}
	output.WriteString("\n" + helpStyle.Render("/ search • L language • r repositories • f5 refresh • esc home"))
	return output.String()
}

func (m Model) extensionListRows(extraRows int) int {
	// Reserve the application title/breadcrumb, Extensions controls, action help,
	// footer, and frame padding so a long inventory never pushes off the chrome.
	return max(1, m.height-16-extraRows)
}
