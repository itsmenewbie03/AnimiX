package mangareader

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"
)

func readerDocumentID(t *testing.T, data []byte) string {
	t.Helper()
	match := regexp.MustCompile(`const documentID = "([a-f0-9]+)";`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatalf("reader lacks document identity: %s", data)
	}
	return string(match[1])
}

func readerEvent(t *testing.T, reader *BrowserReader) ProgressEvent {
	t.Helper()
	select {
	case event := <-reader.Events():
		return event
	case <-time.After(time.Second):
		t.Fatal("reader did not report progress")
		return ProgressEvent{}
	}
}

func TestReaderLivenessRejectsOutOfOrderAndReplacedDocuments(t *testing.T) {
	reader := New(func(string) error { return nil })
	defer reader.Close()
	session, err := reader.Open(Session{Pages: []Page{{URL: "https://example.test/page"}}})
	if err != nil {
		t.Fatal(err)
	}
	openDocument := func() string {
		response, err := http.Get(session.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return readerDocumentID(t, data)
	}
	post := func(document string, sequence int, visible bool) int {
		body := fmt.Sprintf(`{"page":1,"document":%q,"sequence":%d,"visible":%t,"presence_only":true}`, document, sequence, visible)
		response, err := http.Post(session.URL+"/progress", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	document := openDocument()
	if status := post(document, 2, false); status != http.StatusNoContent {
		t.Fatalf("hidden pulse = %d", status)
	}
	event := readerEvent(t, reader)
	if event.Visible || event.SeenAt.IsZero() || !event.PresenceOnly || event.Page != 1 {
		t.Fatalf("liveness observation = %#v", event)
	}
	post(document, 1, true)
	select {
	case event := <-reader.Events():
		t.Fatalf("late visible pulse resurrected a hidden document: %#v", event)
	default:
	}
	post(document, 3, true)
	if event := readerEvent(t, reader); !event.Visible || event.SeenAt.IsZero() {
		t.Fatalf("visible stationary reader = %#v", event)
	}
	replacement := openDocument()
	if replacement == document {
		t.Fatal("reopened reader reused an old document ID")
	}
	if status := post(document, 4, false); status != http.StatusConflict {
		t.Fatalf("old tab close = %d, want conflict", status)
	}
	if status := post("", 5, true); status != http.StatusBadRequest {
		t.Fatalf("anonymous pulse bypassed document ordering: %d", status)
	}
	post(replacement, 1, true)
	if event := readerEvent(t, reader); !event.Visible {
		t.Fatal("old tab hid the replacement reader")
	}
}

func TestReaderCanReopenAfterSessionCloseButNotAfterShutdown(t *testing.T) {
	reader := New(nil)
	defer reader.Close()
	chapter := Session{Pages: []Page{{URL: "https://example.test/page"}}}
	first, err := reader.Open(chapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.CloseSession(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Open(chapter); err != nil {
		t.Fatalf("opening another chapter after closing one failed: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Open(chapter); err == nil {
		t.Fatal("late open resurrected a shut-down reader")
	}
}
