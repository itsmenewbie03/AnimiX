package notification

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	panelWidth = 40
	stepWidth  = 5
)

type Level uint8

const (
	Info Level = iota
	Success
	Warning
	Error
	Debug
	Trace
)

type Notice struct {
	ID      string
	Title   string
	Message string
	Level   Level
	Timeout time.Duration
}

type item struct {
	Notice
	visible    int
	generation int
}

type Model struct {
	items  []item
	nextID int
	accent lipgloss.Color
}

type tickMsg struct {
	id         string
	generation int
}

type dismissMsg struct {
	id         string
	generation int
}

func New(accent lipgloss.Color) Model { return Model{accent: accent} }

func (m *Model) Notify(notice Notice) tea.Cmd {
	if notice.ID == "" {
		m.nextID++
		notice.ID = fmt.Sprintf("notification-%d", m.nextID)
	}
	if notice.Title == "" {
		notice.Title = levelLabel(notice.Level)
	}
	if notice.Timeout == 0 {
		notice.Timeout = 4 * time.Second
	}
	for index := range m.items {
		if m.items[index].ID == notice.ID {
			generation := m.items[index].generation + 1
			m.items[index] = item{Notice: notice, visible: panelWidth, generation: generation}
			return dismissAfter(notice, generation)
		}
	}
	entry := item{Notice: notice, generation: 1}
	m.items = append(m.items, entry)
	return tick(entry.ID, entry.generation)
}

func (m *Model) Hide(id string) {
	for index, entry := range m.items {
		if entry.ID == id {
			m.items = append(m.items[:index], m.items[index+1:]...)
			return
		}
	}
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	switch msg := message.(type) {
	case tickMsg:
		for index := range m.items {
			entry := &m.items[index]
			if entry.ID != msg.id || entry.generation != msg.generation {
				continue
			}
			entry.visible = min(panelWidth, entry.visible+stepWidth)
			if entry.visible < panelWidth {
				return tick(entry.ID, entry.generation)
			}
			return dismissAfter(entry.Notice, entry.generation)
		}
	case dismissMsg:
		for index, entry := range m.items {
			if entry.ID == msg.id && entry.generation == msg.generation {
				m.items = append(m.items[:index], m.items[index+1:]...)
				break
			}
		}
	}
	return nil
}

func (m Model) View() string {
	panels := make([]string, 0, len(m.items))
	for _, entry := range m.items {
		if entry.visible == 0 {
			continue
		}
		panel := m.render(entry)
		lines := strings.Split(panel, "\n")
		for index, line := range lines {
			width := ansi.StringWidth(line)
			lines[index] = ansi.Cut(line, max(0, width-entry.visible), width)
		}
		panels = append(panels, strings.Join(lines, "\n"))
	}
	return strings.Join(panels, "\n")
}

func (m Model) render(entry item) string {
	color, icon := levelAppearance(entry.Level, m.accent)
	header := lipgloss.NewStyle().Bold(true).Foreground(color).Render(icon + entry.Title)
	message := lipgloss.NewStyle().Foreground(lipgloss.Color("#B8B8C8")).Render(entry.Message)
	return lipgloss.NewStyle().
		Width(panelWidth-4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Render(header + "\n" + message)
}

func Overlay(base, popup string, width, row int) string {
	if popup == "" || width <= 0 {
		return base
	}
	baseLines := strings.Split(base, "\n")
	popupLines := strings.Split(popup, "\n")
	for len(baseLines) < row+len(popupLines) {
		baseLines = append(baseLines, "")
	}
	for index, popupLine := range popupLines {
		lineIndex := row + index
		popupWidth := ansi.StringWidth(popupLine)
		leftWidth := max(0, width-popupWidth-1)
		line := baseLines[lineIndex]
		left := ansi.Cut(line, 0, leftWidth)
		left += strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(left)))
		baseLines[lineIndex] = left + popupLine
	}
	return strings.Join(baseLines, "\n")
}

func levelAppearance(level Level, accent lipgloss.Color) (lipgloss.Color, string) {
	switch level {
	case Success:
		return lipgloss.Color("#A6E3A1"), " "
	case Warning:
		return lipgloss.Color("#F9E2AF"), " "
	case Error:
		return lipgloss.Color("#F38BA8"), " "
	case Debug:
		return lipgloss.Color("#89B4FA"), " "
	case Trace:
		return lipgloss.Color("#94E2D5"), " "
	default:
		return accent, " "
	}
}

func levelLabel(level Level) string {
	switch level {
	case Success:
		return "Success"
	case Warning:
		return "Warning"
	case Error:
		return "Error"
	case Debug:
		return "Debug"
	case Trace:
		return "Trace"
	default:
		return "Info"
	}
}

func tick(id string, generation int) tea.Cmd {
	return tea.Tick(18*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{id: id, generation: generation}
	})
}

func dismissAfter(notice Notice, generation int) tea.Cmd {
	if notice.Timeout < 0 {
		return nil
	}
	return tea.Tick(notice.Timeout, func(time.Time) tea.Msg {
		return dismissMsg{id: notice.ID, generation: generation}
	})
}
