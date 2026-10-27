package tui

import (
	"fmt"
	"io"
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

func (m *Model) setEpisodeItems() {
	localWatched := make(map[string]bool)
	for _, number := range m.localWatchedEpisodes() {
		localWatched[normalizedEpisodeNumber(number)] = true
	}
	items := make([]list.Item, 0, len(m.details.Episodes))
	for _, episode := range m.details.Episodes {
		number := normalizedEpisodeNumber(episode.Number())
		aniListWatched := false
		if m.resolvingEpisode != nil {
			value, err := strconv.ParseFloat(number, 64)
			aniListWatched = err == nil && int(value) <= m.resolvingEpisode.Progress
		}
		items = append(items, episodeListItem{
			episode: episode, seriesTitle: m.details.DisplayTitle(),
			aniListWatched: aniListWatched, localWatched: localWatched[number],
		})
	}
	_ = m.episodeList.SetItems(items)
	m.resizeEpisodeList()
}

func (m *Model) resizeEpisodeList() {
	m.episodeList.SetSize(max(32, m.width-8), max(6, m.height-13))
}
