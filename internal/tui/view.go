package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/itsmenewbie03/animix/internal/notification"
)

func (m Model) View() string {
	width := max(40, m.width)
	var body strings.Builder
	body.WriteString(titleStyle.Render("AnimiX"))
	body.WriteString("\n")
	body.WriteString(helpStyle.Render(m.breadcrumb()))
	body.WriteString("\n\n")

	switch m.stage {
	case stageSources:
		body.WriteString(m.sourcesView())
	case stageDiscoverSources:
		body.WriteString(titleStyle.Render("Discover with a source") + "\n\n")
		sources := m.homeSources()
		items := make([]string, len(sources))
		for index, source := range sources {
			items[index] = fmt.Sprintf("%s  %s  [%s]", source.Name, source.Language, strings.ToUpper(source.Type))
		}
		if len(items) == 0 {
			body.WriteString(helpStyle.Render("No installed " + m.homeType + " extensions."))
			body.WriteString("\n\n" + helpStyle.Render("r repositories • esc home"))
		} else {
			body.WriteString(m.listView(items))
			body.WriteString("\n" + helpStyle.Render("enter search with source • esc home"))
		}
	case stageSearch:
		body.WriteString("Search with " + titleStyle.Render(m.selectedSource.Name) + "\n\n")
		body.WriteString(m.search.View())
		body.WriteString("\n\n" + helpStyle.Render("enter search • esc discover"))
	case stageResults:
		body.WriteString(m.mediaResultsView())
		if m.resolvingEpisode != nil {
			body.WriteString("\n" + helpStyle.Render("enter use match • a alternative title • s source • esc back"))
		} else {
			body.WriteString(m.listHelp("open details"))
		}
	case stageDetails:
		body.WriteString(m.detailsView())
	case stageEpisodes:
		if filters := m.chapterScanlatorView(); filters != "" {
			body.WriteString(filters + "\n\n")
		}
		episodes := m.episodeList
		episodes.SetDelegate(episodeDelegate{loading: m.loading, spinnerView: ansi.Strip(m.spinner.View())})
		body.WriteString(episodes.View())
		verb := "resolve pages"
		if m.selectedSource.IsAnime() {
			verb = "resolve streams"
		}
		if m.resolvingEpisode != nil {
			if m.selectedSource.IsAnime() {
				body.WriteString("\n" + helpStyle.Render("󰈈 watched locally • dimmed watched in tracker"))
			} else {
				body.WriteString("\n" + helpStyle.Render("dimmed chapters are read in tracker"))
			}
			body.WriteString("\n" + helpStyle.Render("enter "+verb+" • a wrong title • s source • esc details • q quit"))
		} else {
			body.WriteString(m.listHelp(verb))
		}
	case stageStreams:
		if m.currentMediaID() != 0 {
			check := "[ ]"
			if m.rememberStream {
				check = "[✓]"
			}
			body.WriteString(titleStyle.Render(check+" Remember server") + helpStyle.Render("  •  auto-select this quality for this anime") + "\n\n")
		}
		saved, hasSaved := m.savedStreamValue()
		items := make([]string, len(m.videos))
		for i, video := range m.videos {
			label := firstNonEmpty(streamLabel(video), "Default")
			if hasSaved && streamMatches(video, saved) {
				label += "   Default"
			}
			items[i] = label
		}
		body.WriteString(m.listView(items))
		if m.currentMediaID() != 0 {
			body.WriteString("\n" + helpStyle.Render("↑/↓ or j/k move • space remember • enter play with mpv • esc back • q quit"))
		} else {
			body.WriteString(m.listHelp("play with mpv"))
		}
	case stagePages:
		if m.mangaReader != nil {
			body.WriteString(titleStyle.Render("Browser Reader") + "\n\n")
			if m.activeMangaReading == nil {
				body.WriteString(m.spinner.View() + " Opening chapter in your browser…")
			} else {
				entry := *m.activeMangaReading
				body.WriteString(fmt.Sprintf("Page %d/%d", entry.Page, entry.TotalPages))
			}
			returnLabel := "chapters"
			if m.mangaReaderReturnHome {
				returnLabel = "manga home"
			}
			readerHelp := "esc " + returnLabel + " • q quit"
			if m.activeMangaSessionURL != "" {
				readerHelp = "ctrl+o reopen browser • " + readerHelp
			}
			body.WriteString("\n\n" + helpStyle.Render(readerHelp))
		} else {
			items := make([]string, len(m.pages))
			for i, page := range m.pages {
				items[i] = fmt.Sprintf("Page %d  %s", i+1, page.URL)
			}
			body.WriteString(m.listView(items))
			body.WriteString("\n" + helpStyle.Render("No browser reader configured • esc chapters • q quit"))
		}
	case stageRepositories:
		items := make([]string, len(m.repositories))
		for i, repo := range m.repositories {
			items[i] = fmt.Sprintf("[%s]  %s", strings.ToUpper(repo.Type), repo.URL)
		}
		if len(items) == 0 {
			body.WriteString("No extension repositories configured.\n")
		} else {
			body.WriteString(m.listView(items))
		}
		body.WriteString("\n" + helpStyle.Render("a add • d delete • enter browse • esc sources • q quit"))
	case stageRepositoryAdd:
		body.WriteString("Add extension repository\n\n")
		body.WriteString(m.repositoryInput.View())
		body.WriteString("\n\n" + helpStyle.Render("enter continue • esc repositories"))
	case stageRepositoryType:
		body.WriteString("Repository content type\n\n")
		body.WriteString(helpStyle.Render(m.pendingRepositoryURL))
		body.WriteString("\n\n[a] Anime\n[m] Manga\n\n")
		body.WriteString(helpStyle.Render("a/m select • esc edit URL"))
	case stageAvailableExtensions:
		extensions := m.filteredExtensions()
		language := firstNonEmpty(strings.ToUpper(m.extensionLanguage), "ALL")
		query := firstNonEmpty(m.extensionQuery, "—")
		body.WriteString(helpStyle.Render(fmt.Sprintf("Search: %s  •  Language: %s  •  %d/%d shown", query, language, len(extensions), len(m.availableExtensions))))
		body.WriteString("\n\n")
		items := make([]string, len(extensions))
		for i, extension := range extensions {
			adult := ""
			if extension.NSFW {
				adult = "  NSFW"
			}
			installed := ""
			if m.installer.IsInstalled(extension, m.extensionDirectory) {
				installed = "[✓] "
			}
			items[i] = fmt.Sprintf("%s%s  %s  v%s%s", installed, extension.Name, strings.ToUpper(extension.Language), extension.Version, adult)
		}
		body.WriteString(m.listView(items))
		body.WriteString(m.listHelp("install/update"))
		body.WriteString("\n" + helpStyle.Render("d uninstall • / search • l next language • c clear filters"))
	case stageExtensionSearch:
		extensions := m.filteredExtensions()
		language := firstNonEmpty(strings.ToUpper(m.extensionLanguage), "ALL")
		body.WriteString("Search: " + m.extensionSearch.View())
		body.WriteString("\n" + helpStyle.Render(fmt.Sprintf("Language: %s  •  %d/%d shown", language, len(extensions), len(m.availableExtensions))))
		body.WriteString("\n\n")
		items := make([]string, len(extensions))
		for i, extension := range extensions {
			adult := ""
			if extension.NSFW {
				adult = "  NSFW"
			}
			installed := ""
			if m.installer.IsInstalled(extension, m.extensionDirectory) {
				installed = "[✓] "
			}
			items[i] = fmt.Sprintf("%s%s  %s  v%s%s", installed, extension.Name, strings.ToUpper(extension.Language), extension.Version, adult)
		}
		body.WriteString(m.listView(items))
		body.WriteString("\n" + helpStyle.Render("type to filter live • enter keep filter • esc cancel"))
	case stageAniListToken:
		body.WriteString("Connect AniList\n\n")
		body.WriteString("Press Ctrl+O to authorize with the bundled AniList client, then paste the returned token. It is stored in the CLI config with user-only permissions.\n\n")
		body.WriteString(m.tokenInput.View())
		body.WriteString("\n\n" + helpStyle.Render("ctrl+o open authorization • enter connect • esc sources"))
	case stageAniListLibrary:
		body.WriteString(titleStyle.Render("@" + m.viewer.Name))
		body.WriteString(helpStyle.Render(fmt.Sprintf("  •  %d tracked titles", len(m.trackingEntries))))
		body.WriteString("\n\n")
		items := make([]string, len(m.trackingEntries))
		for i, entry := range m.trackingEntries {
			total := "?"
			if entry.Total > 0 {
				total = fmt.Sprint(entry.Total)
			}
			items[i] = fmt.Sprintf("[%s] %s  %d/%s  %s", entry.Type, entry.Title, entry.Progress, total, entry.Status)
		}
		body.WriteString(m.listView(items))
		body.WriteString("\n" + helpStyle.Render("enter edit • x sign out • esc sources • q quit"))
	case stageAniListEdit:
		entry := m.editingEntry
		total := "?"
		if entry.Total > 0 {
			total = fmt.Sprint(entry.Total)
		}
		body.WriteString(titleStyle.Render(entry.Title))
		body.WriteString("\n\n")
		body.WriteString(fmt.Sprintf("Type: %s\nStatus: %s\nProgress: %d/%s", entry.Type, entry.Status, entry.Progress, total))
		body.WriteString("\n\n" + helpStyle.Render("←/→ or -/+ progress • PgUp/PgDn ±10 • tab/s status • enter save • esc cancel"))
	case stageEpisodeSource:
		label := "anime"
		if m.resolvingEpisode != nil && strings.EqualFold(m.resolvingEpisode.Type, "MANGA") {
			label = "manga"
		}
		body.WriteString(titleStyle.Render("Choose "+label+" source") + "\n\n")
		sources := m.resolutionSources()
		items := make([]string, len(sources))
		for index, source := range sources {
			active := ""
			if source.ID == m.selectedSource.ID {
				active = "  • current"
			}
			items[index] = fmt.Sprintf("%s  %s%s", source.Name, strings.ToUpper(source.Language), active)
		}
		body.WriteString(m.listView(items))
		body.WriteString("\n" + helpStyle.Render("enter select and search • esc episodes"))
	case stageAlternativeTitle:
		body.WriteString(titleStyle.Render("Wrong title?") + "\n\n")
		body.WriteString("Search " + titleStyle.Render(m.selectedSource.Name) + " using a different title.\nChoose the correct result after searching.\n\n")
		body.WriteString(m.alternativeTitle.View())
		body.WriteString("\n\n" + helpStyle.Render("enter search • esc episodes"))
	case stageCloudflareCookie:
		body.WriteString(titleStyle.Render("Cloudflare recovery") + "\n\n")
		body.WriteString("Source: " + m.selectedSource.Name + "\nDomain: " + m.selectedSource.BaseURL + "\n\n")
		body.WriteString("Open the source in the same browser whose User-Agent you will provide, complete the challenge, then copy its Cookie request header. Credentials are stored locally with user-only permissions.\n\n")
		body.WriteString("Cookie header\n" + m.cookieInput.View())
		body.WriteString("\n\n" + helpStyle.Render("ctrl+o open source • ctrl+u clear • enter continue • esc cancel"))
	case stageCloudflareUserAgent:
		body.WriteString(titleStyle.Render("Cloudflare recovery") + "\n\n")
		body.WriteString("Paste the exact User-Agent from the browser that produced the cookie. Cloudflare clearance may be bound to it.\n\n")
		body.WriteString("User-Agent\n" + m.userAgentInput.View())
		body.WriteString("\n\n" + helpStyle.Render("ctrl+o open source • ctrl+u clear • enter apply and retry • esc back"))
	case stageUpdatePrompt:
		body.WriteString(titleStyle.Render("Update AnimiX") + "\n\n")
		body.WriteString(m.updateProgressLabel() + "\n\n")
		body.WriteString(m.updateProgress.ViewAs(m.updatePercent))
	case stageAutoPlayPrompt:
		if m.autoPlay.phase == autoPlayLoadingDetails {
			body.WriteString(m.spinner.View() + " Finding the next episode…")
		} else {
			body.WriteString(titleStyle.Render("Play next episode?") + "\n\n")
			body.WriteString(episodePrimaryLabel(m.autoPlay.next) + "\n\n")
			body.WriteString(m.listView(autoPlayChoices))
			body.WriteString("\n" + helpStyle.Render("↑/↓ or j/k move • enter choose • esc no • q quit"))
		}
	}
	if m.availableUpdate.Version != "" && m.stage != stageUpdatePrompt {
		body.WriteString("\n\n" + helpStyle.Render("u install AnimiX "+m.availableUpdate.Version))
	}
	base := frame(body.String(), width)
	base = notification.Overlay(base, m.notifier.View(), width, 1)
	if m.showKeyHelp {
		return m.keyHelpView()
	}
	return base
}

func (m Model) updateProgressLabel() string {
	label := string(m.updateState.Phase)
	if label == "" {
		label = "Preparing update"
	}
	if m.availableUpdate.Version != "" {
		label += " " + m.availableUpdate.Version
	}
	if m.updateState.Downloaded > 0 {
		label += fmt.Sprintf(" • %.1f MiB", float64(m.updateState.Downloaded)/(1024*1024))
		if m.updateState.Total > 0 {
			label += fmt.Sprintf(" / %.1f MiB • %.0f%% downloaded",
				float64(m.updateState.Total)/(1024*1024), m.updateState.Fraction()*100)
		}
	}
	return label
}

func (m Model) sourcesView() string {
	var output strings.Builder
	animeTab := helpStyle.Render(" 1 Anime ")
	mangaTab := helpStyle.Render(" 2 Manga ")
	if m.homeType == "anime" {
		animeTab = selected.Render("1 Anime")
	} else {
		mangaTab = selected.Render("2 Manga")
	}
	output.WriteString(animeTab + "  " + mangaTab)
	output.WriteString("\n" + helpStyle.Render("1/2 switch media type") + "\n\n")

	if m.homeType == "anime" {
		output.WriteString(homeFeedTab("Resume", len(m.resumeWatchingList.Items()), m.homeFocus == homeFocusResumeWatching) + "  ")
		output.WriteString(homeFeedTab("New Episodes", len(m.newEpisodesList.Items()), m.homeFocus == homeFocusNewEpisodes) + "  ")
		output.WriteString(homeFeedTab("Watching", len(m.continueWatchingList.Items()), m.homeFocus == homeFocusContinueWatching))
		output.WriteString("\n" + helpStyle.Render("tab/shift+tab or h/l switch feed") + "\n\n")

		active := m.activeHomeList()
		if active != nil {
			if active.SettingFilter() {
				output.WriteString(active.FilterInput.View() + "\n\n")
			} else if active.FilterValue() != "" {
				output.WriteString(helpStyle.Render("Filter: "+active.FilterValue()+"  •  / edit  •  c clear") + "\n\n")
			}
			activeList := *active
			loadingSelection := m.loading && m.resolvingEpisode != nil
			if m.homeFocus == homeFocusResumeWatching {
				loadingSelection = m.loading && (m.resolvingEpisode != nil || m.pendingPlayback != nil)
			}
			activeList.SetDelegate(newEpisodeDelegate(true, loadingSelection, ansi.Strip(m.spinner.View())))
			if len(activeList.VisibleItems()) == 0 {
				message := homeFeedEmptyMessage(m.homeFocus)
				if active.FilterValue() != "" {
					message = "No titles match this filter."
				}
				output.WriteString(helpStyle.Render(message) + "\n")
			} else {
				output.WriteString(activeList.View())
			}
		}
	} else {
		feeds := m.mangaFeeds()
		for index, feed := range feeds {
			if index > 0 {
				output.WriteString("  ")
			}
			count := len(m.mangaEntries(feed))
			if feed == mangaFeedResume {
				count = len(m.mangaResumeList.Items())
			}
			output.WriteString(homeFeedTab(mangaFeedLabel(feed), count, m.mangaFeed == feed))
		}
		output.WriteString("\n" + helpStyle.Render("tab/shift+tab or h/l switch feed") + "\n\n")
		active := m.activeHomeList()
		if active.SettingFilter() {
			output.WriteString(active.FilterInput.View() + "\n\n")
		} else if active.FilterValue() != "" {
			output.WriteString(helpStyle.Render("Filter: "+active.FilterValue()+"  •  / edit  •  c clear") + "\n\n")
		}
		activeList := *active
		loadingSelection := m.loading && (m.resolvingEpisode != nil || m.pendingMangaReading != nil)
		activeList.SetDelegate(newEpisodeDelegate(true, loadingSelection, ansi.Strip(m.spinner.View())))
		if len(activeList.VisibleItems()) == 0 {
			message := "No manga entries in this feed."
			if m.mangaFeed == mangaFeedResume {
				message = "No manga to resume. Open a chapter to start reading history."
			}
			if active.FilterValue() != "" {
				message = "No titles match this filter."
			}
			output.WriteString(helpStyle.Render(message) + "\n")
		} else {
			output.WriteString(activeList.View())
		}
	}

	output.WriteString("\n" + homeHelp(m.currentLength() > 0))
	return output.String()
}

func homeFeedTab(label string, count int, active bool) string {
	value := fmt.Sprintf("%s %d", label, count)
	if active {
		return selected.Render(value)
	}
	return helpStyle.Render(" " + value + " ")
}

func (m Model) chapterScanlatorView() string {
	scanlators := m.chapterScanlators()
	if len(scanlators) <= 1 {
		return ""
	}
	counts := map[string]int{"": len(m.details.Episodes)}
	for _, chapter := range m.details.Episodes {
		if scanlator := strings.TrimSpace(chapter.Scanlator); scanlator != "" {
			counts[scanlator]++
		}
	}
	tabs := make([]string, 0, len(scanlators))
	for _, scanlator := range scanlators {
		tabs = append(tabs, homeFeedTab(firstNonEmpty(scanlator, "All"), counts[scanlator], m.chapterScanlator == scanlator))
	}
	return lipgloss.NewStyle().Width(max(32, m.width-8)).Render(
		strings.Join(tabs, "  ") + "\n" + helpStyle.Render("tab/shift+tab or h/l switch scanlator"))
}

func homeFeedEmptyMessage(feed homeFocus) string {
	switch feed {
	case homeFocusResumeWatching:
		return "Nothing to resume. Partially watched episodes appear here."
	case homeFocusNewEpisodes:
		return "No newly released episodes for tracked anime."
	case homeFocusContinueWatching:
		return "No anime currently marked as watching in AniList."
	default:
		return "Nothing to show."
	}
}

func homeHelp(hasItems bool) string {
	controls := "d discover • r repositories • t AniList • ? help • q quit"
	if hasItems {
		controls = "j/k move • gg/G first/last • enter open • / filter • " + controls
	}
	return helpStyle.Render(controls)
}

func continueEpisodeLabel(number, name string) string {
	if name != "" && number != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(number)) {
		return "EP " + number + " - " + name
	}
	if name != "" {
		return name
	}
	if number != "" {
		return "Episode " + number
	}
	return "Current episode"
}

func releaseDateLabel(date *time.Time, now time.Time) string {
	if date == nil {
		return ""
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	releaseDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, now.Location())
	days := int(today.Sub(releaseDay).Hours() / 24)
	switch {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days <= 7:
		return fmt.Sprintf("%d days ago", days)
	case days < 30:
		weeks := days / 7
		return fmt.Sprintf("%d week%s ago", weeks, pluralSuffix(weeks))
	case days < 365:
		months := days / 30
		return fmt.Sprintf("%d month%s ago", months, pluralSuffix(months))
	default:
		years := days / 365
		return fmt.Sprintf("%d year%s ago", years, pluralSuffix(years))
	}
}

func pluralSuffix(value int) string {
	if value == 1 {
		return ""
	}
	return "s"
}

func (m Model) detailsView() string {
	description := strings.TrimSpace(m.details.Description)
	if description == "" {
		description = "No description available."
	}
	if len(description) > 1200 {
		description = description[:1200] + "…"
	}
	itemLabel := "Episodes"
	openLabel := "episodes"
	if strings.EqualFold(m.selectedSource.Type, "manga") {
		itemLabel, openLabel = "Chapters", "chapters"
	}
	content := titleStyle.Render(m.details.DisplayTitle()) + "\n\n" +
		lipgloss.NewStyle().Width(max(30, m.width-8)).Render(description) + "\n\n" +
		fmt.Sprintf("%s: %d", itemLabel, len(m.details.Episodes))
	if len(m.details.Genres) > 0 {
		content += "\nGenres: " + strings.Join(m.details.Genres, ", ")
	}
	if m.resolvingEpisode != nil {
		content += "\n\n" + helpStyle.Render("enter "+openLabel+" • a wrong title • s source • esc matches • q quit")
	} else {
		content += "\n\n" + helpStyle.Render("enter "+openLabel+" • esc results • q quit")
	}
	return content
}

func (m Model) listView(items []string) string {
	return m.listViewFocused(items, true)
}

func (m Model) listViewFocused(items []string, focused bool) string {
	if len(items) == 0 {
		return helpStyle.Render("No results.")
	}
	available := max(5, m.height-10)
	start := max(0, m.cursor-available/2)
	end := min(len(items), start+available)
	if end-start < available {
		start = max(0, end-available)
	}
	var output strings.Builder
	for i := start; i < end; i++ {
		selectedRow := focused && i == m.cursor
		selector := "  "
		if selectedRow {
			selector = "┃ "
			if m.loading {
				selector = ansi.Strip(m.spinner.View()) + " "
			}
		}
		availableWidth := max(12, m.width-lipgloss.Width(selector)-8)
		label := ansi.Truncate(items[i], availableWidth, "…")
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E6F0"))
		if selectedRow {
			labelStyle = labelStyle.Foreground(accent).Bold(true)
		}
		output.WriteString(lipgloss.NewStyle().Foreground(accent).Render(selector))
		if strings.Contains(label, "[✓]") {
			output.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Bold(true).Render(label))
		} else {
			output.WriteString(labelStyle.Render(label))
		}
		output.WriteString("\n")
	}
	return output.String()
}

func (m Model) listHelp(action string) string {
	return "\n" + helpStyle.Render("↑/↓ or j/k move • enter "+action+" • esc back • q quit")
}

func (m Model) breadcrumb() string {
	if m.stage == stageAutoPlayPrompt {
		return "Home › Playback › Next episode"
	}
	if m.stage == stageDiscoverSources {
		return "Home › Discover"
	}
	if m.stage == stageEpisodeSource || m.stage == stageAlternativeTitle {
		label := "Source"
		if m.stage == stageAlternativeTitle {
			label = "Alternative title"
		}
		mediaType := "Anime"
		if m.resolvingEpisode != nil && strings.EqualFold(m.resolvingEpisode.Type, "MANGA") {
			mediaType = "Manga"
		}
		return strings.Join([]string{"Home", mediaType, label}, " › ")
	}
	if m.stage == stageAniListToken || m.stage == stageAniListLibrary || m.stage == stageAniListEdit {
		parts := []string{"Home", "AniList"}
		if m.stage == stageAniListEdit {
			parts = append(parts, m.editingEntry.Title)
		}
		return strings.Join(parts, " › ")
	}
	if m.stage == stageRepositories || m.stage == stageRepositoryAdd || m.stage == stageRepositoryType || m.stage == stageAvailableExtensions || m.stage == stageExtensionSearch {
		parts := []string{"Home", "Repositories"}
		if m.stage == stageRepositoryAdd || m.stage == stageRepositoryType {
			parts = append(parts, "Add")
		}
		if m.stage == stageAvailableExtensions || m.stage == stageExtensionSearch {
			parts = append(parts, "Available")
		}
		if m.stage == stageExtensionSearch {
			parts = append(parts, "Search")
		}
		return strings.Join(parts, " › ")
	}
	contentStage := m.stage
	if m.stage == stageCloudflareCookie || m.stage == stageCloudflareUserAgent {
		contentStage = m.cloudflareReturn
	}
	parts := []string{"Home"}
	if contentStage >= stageSearch {
		parts = append(parts, m.selectedSource.Name)
	}
	if contentStage >= stageResults {
		parts = append(parts, "Results")
	}
	if contentStage >= stageDetails && m.details.DisplayTitle() != "" {
		parts = append(parts, m.details.DisplayTitle())
	}
	if contentStage >= stageEpisodes {
		if strings.EqualFold(m.selectedSource.Type, "manga") {
			parts = append(parts, "Chapters")
		} else {
			parts = append(parts, "Episodes")
		}
	}
	if contentStage == stageStreams {
		parts = append(parts, "Streams")
	}
	if contentStage == stagePages {
		parts = append(parts, "Pages")
	}
	return strings.Join(parts, " › ")
}

func frame(content string, width int) string {
	return lipgloss.NewStyle().Padding(1, 2).Width(width - 4).Render(content)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
