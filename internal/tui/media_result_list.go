package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) mediaResultsView() string {
	if len(m.results) == 0 {
		return helpStyle.Render("No results.")
	}
	visible := max(5, m.height-10)
	start := max(0, m.cursor-visible/2)
	end := min(len(m.results), start+visible)
	if end-start < visible {
		start = max(0, end-visible)
	}
	var output strings.Builder
	for index := start; index < end; index++ {
		selectedRow := index == m.cursor
		selector := "  "
		if selectedRow {
			selector = "┃ "
			if m.loading {
				selector = ansi.Strip(m.spinner.View()) + " "
			}
		}
		markerStyle := lipgloss.NewStyle().Foreground(accent)
		titleColor := lipgloss.Color("#E8E6F0")
		if selectedRow {
			titleColor = accent
		}
		availableWidth := max(12, m.width-lipgloss.Width(selector)-8)
		title := ansi.Truncate(m.results[index].DisplayTitle(), availableWidth, "…")
		titleStyle := lipgloss.NewStyle().Foreground(titleColor)
		if selectedRow {
			titleStyle = titleStyle.Bold(true)
		}
		fmt.Fprintf(&output, "%s%s", markerStyle.Render(selector), titleStyle.Render(title))
		if index+1 < end {
			output.WriteString("\n")
		}
	}
	return output.String()
}
