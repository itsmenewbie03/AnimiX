package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestNotifyReplacesStableID(t *testing.T) {
	t.Parallel()
	model := New(lipgloss.Color("#9B87F5"))
	model.Notify(Notice{ID: "update", Message: "10%", Timeout: -1})
	model.Notify(Notice{ID: "update", Message: "20%", Timeout: -1})
	if len(model.items) != 1 || model.items[0].Message != "20%" {
		t.Fatalf("notifications = %#v", model.items)
	}
}

func TestNotificationSlidesAndDismisses(t *testing.T) {
	t.Parallel()
	model := New(lipgloss.Color("#9B87F5"))
	model.Notify(Notice{ID: "update", Title: "Update", Message: "Ready", Timeout: time.Second})
	generation := model.items[0].generation
	for model.items[0].visible < panelWidth {
		model.Update(tickMsg{id: "update", generation: generation})
	}
	if !strings.Contains(model.View(), "Ready") {
		t.Fatalf("notification view = %q", model.View())
	}
	model.Update(dismissMsg{id: "update", generation: generation})
	if len(model.items) != 0 {
		t.Fatalf("notification not dismissed: %#v", model.items)
	}
}

func TestOverlayDoesNotInsertRowsAboveContent(t *testing.T) {
	t.Parallel()
	base := "title\ncontent\nfooter"
	overlaid := Overlay(base, "notice", 20, 1)
	lines := strings.Split(overlaid, "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "title") || !strings.Contains(lines[2], "footer") {
		t.Fatalf("overlay shifted base layout: %q", overlaid)
	}
}
