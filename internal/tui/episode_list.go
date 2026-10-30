package tui

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/itsmenewbie03/animix/internal/catalog"
)

type episodeDelegate struct {
	loading     bool
	spinnerView string
}

func (episodeDelegate) Height() int                         { return 2 }
func (episodeDelegate) Spacing() int                        { return 1 }
func (episodeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (delegate episodeDelegate) Render(writer io.Writer, model list.Model, index int, raw list.Item) {
	item, ok := raw.(episodeListItem)
	if !ok {
		return
	}
	selectedRow := index == model.Index()
	selector := "  "
	if selectedRow {
		selector = "┃ "
		if delegate.loading && delegate.spinnerView != "" {
			selector = delegate.spinnerView + " "
		}
	}
	localMarker := "  "
	if item.localWatched {
		localMarker = "󰈈 "
	}

	titleColor := lipgloss.Color("#E8E6F0")
	descriptionColor := dim
	if item.aniListWatched || item.localWatched {
		titleColor = lipgloss.Color("#707078")
		descriptionColor = lipgloss.Color("#62626A")
	}
	if selectedRow {
		titleColor = accent
		descriptionColor = lipgloss.Color("#C9C2F8")
	}

	availableWidth := max(8, model.Width()-lipgloss.Width(selector)-lipgloss.Width(localMarker)-2)
	title := ansi.Truncate(item.Title(), availableWidth, "…")
	description := ansi.Truncate(item.Description(), availableWidth, "…")
	markerStyle := lipgloss.NewStyle().Foreground(accent)
	titleStyle := lipgloss.NewStyle().Foreground(titleColor)
	if selectedRow {
		titleStyle = titleStyle.Bold(true)
	}
	localMarkerStyle := lipgloss.NewStyle().Foreground(descriptionColor)
	descriptionStyle := lipgloss.NewStyle().Foreground(descriptionColor)
	descriptionSelector := "  "
	if selectedRow {
		descriptionSelector = "┃ "
	}
	fmt.Fprintf(writer, "%s%s%s\n%s%s%s",
		markerStyle.Render(selector), localMarkerStyle.Render(localMarker), titleStyle.Render(title),
		markerStyle.Render(descriptionSelector), "  ", descriptionStyle.Render(description))
}

func episodePrimaryLabel(episode catalog.Episode) string {
	if number := normalizedEpisodeNumber(episode.Number()); number != "" {
		return "Episode " + number
	}
	return firstNonEmpty(episode.Name, "Episode")
}

func chapterPrimaryLabel(chapter catalog.Episode) string {
	if number := normalizedEpisodeNumber(chapter.Number()); number != "" {
		return "Chapter " + number
	}
	return firstNonEmpty(chapter.Name, "Chapter")
}

func chapterSubtitle(chapter catalog.Episode, seriesTitle string) string {
	var title string
	for _, value := range []string{chapter.Title, chapter.Name, chapter.Summary, chapter.Description, chapter.Memo} {
		if title = cleanChapterTitle(value, chapter.Number()); title != "" {
			break
		}
	}
	if scanlator := strings.TrimSpace(chapter.Scanlator); scanlator != "" {
		if title == "" {
			return scanlator
		}
		return scanlator + " • " + title
	}
	return firstNonEmpty(title, strings.TrimSpace(seriesTitle))
}

func cleanChapterTitle(value, number string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "{}", "[]", "null":
		return ""
	}
	number = normalizedEpisodeNumber(number)
	for _, prefix := range []string{"Chapter " + number, "Ch. " + number, "Ch " + number, "Episode " + number} {
		if number != "" && len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
			return strings.TrimLeft(strings.TrimSpace(value[len(prefix):]), "-–—:| ")
		}
	}
	return value
}

func episodeSubtitle(episode catalog.Episode, seriesTitle string) string {
	if title := strings.TrimSpace(episode.Title); title != "" {
		return title
	}
	primary := episodePrimaryLabel(episode)
	name := strings.TrimSpace(episode.Name)
	if name != "" && !strings.EqualFold(name, primary) {
		lowerName, lowerPrimary := strings.ToLower(name), strings.ToLower(primary)
		if strings.HasPrefix(lowerName, lowerPrimary) {
			name = strings.TrimLeft(strings.TrimSpace(name[len(primary):]), "-–—:| ")
		}
		if name != "" && !strings.EqualFold(name, primary) {
			return name
		}
	}
	return strings.TrimSpace(firstNonEmpty(episode.Summary, episode.Description, episode.Memo, seriesTitle, episodePrimaryLabel(episode)))
}

func playbackMediaTitle(seriesTitle string, episode catalog.Episode) string {
	seriesTitle = strings.TrimSpace(seriesTitle)
	number := normalizedEpisodeNumber(episode.Number())
	episodeTitle := strings.TrimSpace(episode.Title)
	if strings.EqualFold(episodeTitle, episodePrimaryLabel(episode)) {
		episodeTitle = ""
	}
	if episodeTitle != "" {
		label := episodeTitle
		if number != "" {
			label += " (EP " + number + ")"
		}
		if seriesTitle == "" {
			return label
		}
		return seriesTitle + " — " + label
	}
	if number != "" {
		if seriesTitle == "" {
			return "Episode " + number
		}
		return seriesTitle + " — Episode " + number
	}
	return firstNonEmpty(seriesTitle, strings.TrimSpace(episode.Name))
}

func (m *Model) setEpisodeItems() {
	manga := strings.EqualFold(m.selectedSource.Type, "manga")
	if !slices.Contains(m.chapterScanlators(), m.chapterScanlator) {
		m.chapterScanlator = ""
	}
	localWatched := make(map[string]bool)
	for _, number := range m.localWatchedEpisodes() {
		localWatched[normalizedEpisodeNumber(number)] = true
	}
	items := make([]list.Item, 0, len(m.details.Episodes))
	for _, episode := range m.details.Episodes {
		if manga && m.chapterScanlator != "" && strings.TrimSpace(episode.Scanlator) != m.chapterScanlator {
			continue
		}
		number := normalizedEpisodeNumber(episode.Number())
		aniListWatched := false
		if m.resolvingEpisode != nil {
			value, err := strconv.ParseFloat(number, 64)
			aniListWatched = err == nil && int(value) <= m.resolvingEpisode.Progress
		}
		items = append(items, episodeListItem{
			episode: episode, seriesTitle: m.details.DisplayTitle(),
			manga:          manga,
			aniListWatched: aniListWatched, localWatched: localWatched[number],
		})
	}
	_ = m.episodeList.SetItems(items)
	m.resizeEpisodeList()
}

func (m *Model) resizeEpisodeList() {
	height := m.height - 13
	if filters := m.chapterScanlatorView(); filters != "" {
		height -= lipgloss.Height(filters) + 1
	}
	m.episodeList.SetSize(max(32, m.width-8), max(6, height))
}

// The empty scanlator represents All; named groups retain the source's order.
func (m Model) chapterScanlators() []string {
	if !strings.EqualFold(m.selectedSource.Type, "manga") {
		return nil
	}
	scanlators := []string{""}
	seen := map[string]bool{"": true}
	for _, chapter := range m.details.Episodes {
		scanlator := strings.TrimSpace(chapter.Scanlator)
		if !seen[scanlator] {
			seen[scanlator] = true
			scanlators = append(scanlators, scanlator)
		}
	}
	return scanlators
}

func (m *Model) cycleChapterScanlator(delta int) {
	scanlators := m.chapterScanlators()
	if len(scanlators) <= 1 {
		return
	}
	previous, hadSelection := m.episodeList.SelectedItem().(episodeListItem)
	index := max(0, slices.Index(scanlators, m.chapterScanlator))
	m.chapterScanlator = scanlators[(index+delta+len(scanlators))%len(scanlators)]
	m.setEpisodeItems()
	m.cursor = 0
	if hadSelection {
		number := normalizedEpisodeNumber(previous.episode.Number())
		matchedNumber := false
		for index, raw := range m.episodeList.Items() {
			chapter := raw.(episodeListItem).episode
			if previous.episode.URL != "" && chapter.URL == previous.episode.URL {
				m.cursor = index
				break
			}
			if !matchedNumber && number != "" && normalizedEpisodeNumber(chapter.Number()) == number {
				m.cursor = index
				matchedNumber = true
			}
		}
	}
	m.episodeList.Select(m.cursor)
}
