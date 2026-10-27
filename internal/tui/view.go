package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) View() string {
	width := max(40, m.width)
	var body strings.Builder
	body.WriteString(titleStyle.Render("AnimiX"))
	body.WriteString("\n")
	body.WriteString(helpStyle.Render(m.breadcrumb()))
	body.WriteString("\n\n")

	if m.err != nil {
		body.WriteString(errorStyle.Render(m.err.Error()))
		body.WriteString("\n\n")
	}
	if m.status != "" {
		body.WriteString(helpStyle.Render(m.status))
		body.WriteString("\n\n")
	}

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
		items := make([]string, len(m.results))
		for i, media := range m.results {
			items[i] = media.DisplayTitle()
		}
		body.WriteString(m.listView(items))
		if m.resolvingEpisode != nil {
			body.WriteString("\n" + helpStyle.Render("enter use match • a alternative title • s source • esc back"))
		} else {
			body.WriteString(m.listHelp("open details"))
		}
	case stageDetails:
		body.WriteString(m.detailsView())
	case stageEpisodes:
		episodes := m.episodeList
		episodes.SetDelegate(episodeDelegate{loading: m.loading, spinnerView: ansi.Strip(m.spinner.View())})
		body.WriteString(episodes.View())
		verb := "resolve pages"
		if m.selectedSource.IsAnime() {
			verb = "resolve streams"
		}
		if m.resolvingEpisode != nil {
			body.WriteString("\n" + helpStyle.Render("󰈈 watched locally • dimmed watched in tracker"))
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
		items := make([]string, len(m.pages))
		for i, page := range m.pages {
			items[i] = fmt.Sprintf("Page %d  %s", i+1, page.URL)
		}
		body.WriteString(m.listView(items))
		body.WriteString("\n" + helpStyle.Render("Page viewer/download support is next • esc episodes • q quit"))
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
				installed = "[installed] "
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
				installed = "[installed] "
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
		body.WriteString(titleStyle.Render("Choose anime source") + "\n\n")
		sources := m.animeSources()
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
	}
	return frame(body.String(), width)
}

func (m Model) sourcesView() string {
	var output strings.Builder
	animeTab := helpStyle.Render(" Anime ")
	mangaTab := helpStyle.Render(" Manga ")
	if m.homeType == "anime" {
		animeTab = selected.Render("Anime")
	} else {
		mangaTab = selected.Render("Manga")
	}
	output.WriteString(animeTab + "  " + mangaTab)
	output.WriteString("\n" + helpStyle.Render("h/l or tab/←/→ switch • 1 anime • 2 manga") + "\n\n")

	if m.homeType == "anime" {
		loadingSelection := m.loading && m.resolvingEpisode != nil
		resumeLoading := m.loading && m.pendingPlayback != nil
		if len(m.resumeWatchingList.Items()) > 0 {
			output.WriteString(homeSectionTitle("Resume Watching", m.homeFocus == homeFocusResumeWatching) + "\n")
			resumeList := m.resumeWatchingList
			resumeList.SetDelegate(newEpisodeDelegate(m.homeFocus == homeFocusResumeWatching, loadingSelection || resumeLoading, ansi.Strip(m.spinner.View())))
			output.WriteString(resumeList.View())
			output.WriteString("\n")
		}
		if len(m.newEpisodesList.Items()) > 0 {
			output.WriteString(homeSectionTitle("New Episodes", m.homeFocus == homeFocusNewEpisodes) + "\n")
			episodeList := m.newEpisodesList
			episodeList.SetDelegate(newEpisodeDelegate(m.homeFocus == homeFocusNewEpisodes, loadingSelection, ansi.Strip(m.spinner.View())))
			output.WriteString(episodeList.View())
			output.WriteString("\n")
		}
		if len(m.continueWatchingList.Items()) > 0 {
			output.WriteString(homeSectionTitle("Continue Watching", m.homeFocus == homeFocusContinueWatching) + "\n")
			continueList := m.continueWatchingList
			continueList.SetDelegate(newEpisodeDelegate(m.homeFocus == homeFocusContinueWatching, loadingSelection, ansi.Strip(m.spinner.View())))
			output.WriteString(continueList.View())
			output.WriteString("\n")
		}
	}

	hasHomeItems := m.homeType == "anime" && (len(m.resumeWatchingList.Items()) > 0 || len(m.newEpisodesList.Items()) > 0 || len(m.continueWatchingList.Items()) > 0)
	output.WriteString(homeHelp(hasHomeItems))
	return output.String()
}

func homeSectionTitle(label string, focused bool) string {
	marker, style := "◇", helpStyle
	if focused {
		marker, style = "◆", titleStyle
	}
	return style.Render(marker + " " + label)
}

func homeHelp(hasNewEpisodes bool) string {
	controls := "h/l tabs • d discover • r extensions • t AniList • q quit"
	if hasNewEpisodes {
		controls = "↑/↓ or j/k move • enter open • " + controls
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
	content := titleStyle.Render(m.details.DisplayTitle()) + "\n\n" +
		lipgloss.NewStyle().Width(max(30, m.width-8)).Render(description) + "\n\n" +
		fmt.Sprintf("Episodes/chapters: %d", len(m.details.Episodes))
	if len(m.details.Genres) > 0 {
		content += "\nGenres: " + strings.Join(m.details.Genres, ", ")
	}
	if m.resolvingEpisode != nil {
		content += "\n\n" + helpStyle.Render("enter episodes • a wrong title • s source • esc matches • q quit")
	} else {
		content += "\n\n" + helpStyle.Render("enter episodes/chapters • esc results • q quit")
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
		label := items[i]
		if len(label) > max(20, m.width-8) {
			label = label[:max(17, m.width-11)] + "…"
		}
		if focused && i == m.cursor {
			marker := "  "
			if m.loading {
				marker = m.spinner.View() + " "
			}
			output.WriteString(selected.Padding(0).Render(marker + label))
		} else {
			output.WriteString("  " + label)
		}
		output.WriteString("\n")
	}
	return output.String()
}

func (m Model) listHelp(action string) string {
	return "\n" + helpStyle.Render("↑/↓ or j/k move • enter "+action+" • esc back • q quit")
}

func (m Model) breadcrumb() string {
	if m.stage == stageDiscoverSources {
		return "Home › Discover"
	}
	if m.stage == stageEpisodeSource || m.stage == stageAlternativeTitle {
		label := "Source"
		if m.stage == stageAlternativeTitle {
			label = "Alternative title"
		}
		return strings.Join([]string{"Home", "New Episodes", label}, " › ")
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
		parts = append(parts, "Episodes")
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
