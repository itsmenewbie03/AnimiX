package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var homeFeeds = []homeFocus{
	homeFocusResumeWatching,
	homeFocusNewEpisodes,
	homeFocusContinueWatching,
}

func replaceHomeItems(model *list.Model, items []list.Item) {
	filter := model.FilterValue()
	wasFiltering := model.SettingFilter()
	_ = model.SetItems(items)
	if filter != "" {
		model.SetFilterText(filter)
		if wasFiltering {
			model.SetFilterState(list.Filtering)
		}
	}
}

func (m *Model) activeHomeList() *list.Model {
	if m.homeType == "manga" {
		if m.mangaFeed == mangaFeedResume {
			return &m.mangaResumeList
		}
		return &m.mangaTrackingList
	}
	switch m.homeFocus {
	case homeFocusResumeWatching:
		return &m.resumeWatchingList
	case homeFocusNewEpisodes:
		return &m.newEpisodesList
	case homeFocusContinueWatching:
		return &m.continueWatchingList
	default:
		return nil
	}
}

func (m *Model) cycleHomeFeed(delta int) {
	if m.homeType == "manga" {
		feeds := m.mangaFeeds()
		if len(feeds) == 0 {
			return
		}
		index := 0
		for i, feed := range feeds {
			if feed == m.mangaFeed {
				index = i
				break
			}
		}
		m.mangaFeed = feeds[(index+delta+len(feeds))%len(feeds)]
		m.refreshMangaTrackingList()
		return
	}
	if m.homeType != "anime" {
		return
	}
	index := 0
	for i, feed := range homeFeeds {
		if feed == m.homeFocus {
			index = i
			break
		}
	}
	index = (index + delta + len(homeFeeds)) % len(homeFeeds)
	m.homeFocus = homeFeeds[index]
}

func (m *Model) beginHomeFilter() tea.Cmd {
	active := m.activeHomeList()
	if active == nil {
		return nil
	}
	active.SetFilterState(list.Filtering)
	return active.FilterInput.Cursor.BlinkCmd()
}

func (m *Model) updateHomeFilter(key tea.KeyMsg) tea.Cmd {
	active := m.activeHomeList()
	if active == nil {
		return nil
	}
	updated, command := active.Update(key)
	*active = updated
	return command
}

func (m *Model) clearHomeFilter() {
	if active := m.activeHomeList(); active != nil {
		active.ResetFilter()
	}
}

func (m Model) homeFilterActive() bool {
	active := m.activeHomeList()
	return active != nil && active.FilterValue() != ""
}

func (m *Model) jumpToFirst() {
	if m.stage == stageSources {
		if active := m.activeHomeList(); active != nil {
			active.Select(0)
		}
		return
	}
	if m.stage == stageEpisodes {
		m.episodeList.Select(0)
		m.cursor = 0
		return
	}
	m.cursor = 0
}

func (m *Model) jumpToLast() {
	if m.stage == stageSources {
		if active := m.activeHomeList(); active != nil {
			active.Select(max(0, len(active.VisibleItems())-1))
		}
		return
	}
	if m.stage == stageEpisodes {
		last := max(0, len(m.episodeList.VisibleItems())-1)
		m.episodeList.Select(last)
		m.cursor = last
		return
	}
	m.cursor = max(0, m.currentLength()-1)
}

func (m *Model) movePage(delta int) {
	step := max(1, m.height-12)
	if m.stage == stageSources {
		step = max(1, (m.height-12)/3)
		if active := m.activeHomeList(); active != nil {
			active.Select(max(0, min(len(active.VisibleItems())-1, active.Index()+delta*step)))
		}
		return
	}
	if m.stage == stageEpisodes {
		step = max(1, (m.height-10)/3)
		last := max(0, len(m.episodeList.VisibleItems())-1)
		m.episodeList.Select(max(0, min(last, m.episodeList.Index()+delta*step)))
		m.cursor = m.episodeList.Index()
		return
	}
	m.cursor = max(0, min(m.currentLength()-1, m.cursor+delta*step))
}

type keyHelpSection struct {
	title    string
	bindings [][2]string
}

var keyHelpSections = []keyHelpSection{
	{title: "GLOBAL", bindings: [][2]string{
		{"?", "Toggle keyboard help"},
		{"Esc / Backspace", "Back or cancel"},
		{"q", "Quit"},
		{"Ctrl+C", "Force quit"},
		{"u", "Install an available AnimiX update"},
	}},
	{title: "NAVIGATION", bindings: [][2]string{
		{"↑/↓ or j/k", "Move selection"},
		{"gg / G", "First / last item"},
		{"PgUp/PgDn", "Move one viewport"},
		{"Enter", "Open or select"},
	}},
	{title: "HOME", bindings: [][2]string{
		{"1 / 2", "Anime / Manga"},
		{"Tab / Shift+Tab", "Next / previous feed"},
		{"←/→ or h/l", "Previous / next feed"},
		{"/", "Filter active feed"},
		{"c", "Clear active feed filter"},
		{"d", "Discover with a source"},
		{"r", "Extension repositories"},
		{"t", "AniList"},
		{"p", "Discord presence"},
	}},
	{title: "ANIME & PLAYBACK", bindings: [][2]string{
		{"a", "Use an alternative title"},
		{"s", "Change source"},
		{"c", "Cloudflare source recovery"},
		{"Space", "Remember selected server"},
	}},
	{title: "MANGA CHAPTERS", bindings: [][2]string{
		{"Tab / Shift+Tab", "Next / previous scanlator"},
		{"←/→ or h/l", "Previous / next scanlator"},
	}},
	{title: "MANGA READER", bindings: [][2]string{
		{"Ctrl+O", "Reopen the browser reader"},
	}},
	{title: "ANILIST EDITOR", bindings: [][2]string{
		{"←/→ or -/+", "Progress −1 / +1"},
		{"PgDn/PgUp", "Progress −10 / +10"},
		{"Tab or s", "Cycle status"},
		{"x", "Sign out from the library"},
	}},
	{title: "REPOSITORIES & EXTENSIONS", bindings: [][2]string{
		{"a", "Add repository"},
		{"d", "Delete repository or uninstall extension"},
		{"/", "Filter extensions"},
		{"l", "Next extension language"},
		{"c", "Clear extension filters"},
	}},
	{title: "TEXT FIELDS", bindings: [][2]string{
		{"Enter", "Submit"},
		{"Esc", "Cancel"},
		{"Ctrl+O", "Open authorization/source page"},
		{"Ctrl+U", "Clear Cloudflare input"},
	}},
}

func (m Model) maxKeyHelpScroll() int {
	lineCount := max(0, len(keyHelpSections)-1)
	for _, section := range keyHelpSections {
		lineCount += 1 + len(section.bindings)
	}
	return max(0, lineCount-max(8, m.height-8))
}

func (m Model) keyHelpView() string {
	lines := make([]string, 0, 48)
	for sectionIndex, section := range keyHelpSections {
		if sectionIndex > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, titleStyle.Render(section.title))
		for _, binding := range section.bindings {
			lines = append(lines, fmt.Sprintf("%-19s %s", binding[0], binding[1]))
		}
	}
	available := max(8, m.height-8)
	maxScroll := m.maxKeyHelpScroll()
	start := min(maxScroll, max(0, m.keyHelpScroll))
	end := min(len(lines), start+available)
	visible := append([]string(nil), lines[start:end]...)
	if start > 0 {
		visible[0] = helpStyle.Render("↑ more")
	}
	if end < len(lines) {
		visible[len(visible)-1] = helpStyle.Render("↓ more")
	}
	content := titleStyle.Render("Keyboard shortcuts") + "\n\n" + strings.Join(visible, "\n") +
		"\n\n" + helpStyle.Render("↑/↓ scroll • ?/esc/q close")
	popupWidth := min(68, max(42, m.width-8))
	popup := lipgloss.NewStyle().
		Width(popupWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2).
		Render(content)
	return lipgloss.Place(max(40, m.width), max(16, m.height), lipgloss.Center, lipgloss.Center, popup)
}
