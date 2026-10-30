package mangareader

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBrowserReaderServesPagesAndReportsProgress(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Referer"); got != "https://manga.example/title" {
			t.Errorf("Referer = %q", got)
		}
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write([]byte("page-image"))
	}))
	defer upstream.Close()

	var openedURL string
	reader := New(func(value string) error {
		openedURL = value
		return nil
	})
	defer reader.Close()

	opened, err := reader.Open(Session{
		Title: "Example Manga", Chapter: "Chapter 5", StartPage: 2,
		Pages: []Page{
			{URL: upstream.URL + "/1", Headers: map[string]string{"Referer": "https://manga.example/title"}},
			{URL: upstream.URL + "/2", Headers: map[string]string{"Referer": "https://manga.example/title"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if openedURL != opened.URL || opened.ID == "" {
		t.Fatalf("opened session = %#v, browser URL = %q", opened, openedURL)
	}

	response, err := http.Get(opened.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if !strings.Contains(page, "Example Manga") || !strings.Contains(page, `data-start-page="2"`) {
		t.Fatalf("reader page missing session data:\n%s", page)
	}
	pagePath := "/session/" + opened.ID + "/page/0"
	progressPath := "/session/" + opened.ID + "/progress"
	if !strings.Contains(page, `src="`+pagePath+`"`) || !strings.Contains(page, `fetch('`+progressPath+`'`) {
		t.Fatalf("reader assets do not retain the session ID:\n%s", page)
	}
	for _, behavior := range []string{"controls-hidden", "lastScrollY", "pointermove"} {
		if !strings.Contains(page, behavior) {
			t.Fatalf("reader page lacks auto-hiding controls behavior %q:\n%s", behavior, page)
		}
	}
	observeIndex := strings.Index(page, "forEach(page => observer.observe(page))")
	loadIndex := strings.Index(page, "window.addEventListener('load'")
	if observeIndex < 0 || loadIndex < 0 || observeIndex < loadIndex {
		t.Fatalf("reader observes pages before restoring the saved position:\n%s", page)
	}

	imageResponse, err := http.Get(opened.URL + "/page/1")
	if err != nil {
		t.Fatal(err)
	}
	image, err := io.ReadAll(imageResponse.Body)
	imageResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if imageResponse.Header.Get("Content-Type") != "image/png" || string(image) != "page-image" {
		t.Fatalf("proxied image = content-type %q body %q", imageResponse.Header.Get("Content-Type"), image)
	}

	faviconResponse, err := http.Get(strings.TrimSuffix(opened.URL, "/session/"+opened.ID) + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	faviconResponse.Body.Close()
	if faviconResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("favicon status = %d", faviconResponse.StatusCode)
	}

	progressBody := fmt.Sprintf(`{"page":2,"document":%q,"sequence":1,"visible":true}`, readerDocumentID(t, body))
	progressResponse, err := http.Post(opened.URL+"/progress", "application/json", bytes.NewBufferString(progressBody))
	if err != nil {
		t.Fatal(err)
	}
	progressResponse.Body.Close()
	select {
	case event := <-reader.Events():
		if event.SessionID != opened.ID || event.Page != 2 || event.Total != 2 {
			t.Fatalf("progress event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not report progress")
	}
}

func TestCloseSessionStopsLoopbackServerWhenReaderIsEmpty(t *testing.T) {
	t.Parallel()
	reader := New(func(string) error { return nil })
	defer reader.Close()
	opened, err := reader.Open(Session{Pages: []Page{{URL: "https://example.invalid/page"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.CloseSession(opened.ID); err != nil {
		t.Fatal(err)
	}
	readerURL, err := url.Parse(opened.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, dialErr := net.DialTimeout("tcp", readerURL.Host, 100*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		t.Fatal("loopback reader port still accepts connections")
	}
	reader.mutex.RLock()
	serverRunning := reader.server != nil
	_, sessionExists := reader.sessions[opened.ID]
	reader.mutex.RUnlock()
	if serverRunning || sessionExists {
		t.Fatalf("reader retained closed session: server=%v session=%v", serverRunning, sessionExists)
	}
}

func TestOpenFailureStopsEmptyLoopbackServer(t *testing.T) {
	t.Parallel()
	var openedURL string
	reader := New(func(value string) error {
		openedURL = value
		return errors.New("browser failed")
	})
	defer reader.Close()

	if _, err := reader.Open(Session{Pages: []Page{{URL: "https://example.invalid/page"}}}); err == nil {
		t.Fatal("Open succeeded despite browser failure")
	}
	readerURL, err := url.Parse(openedURL)
	if err != nil {
		t.Fatal(err)
	}
	connection, dialErr := net.DialTimeout("tcp", readerURL.Host, 100*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		t.Fatal("failed browser open left loopback listener running")
	}
	reader.mutex.RLock()
	serverRunning := reader.server != nil
	sessionCount := len(reader.sessions)
	reader.mutex.RUnlock()
	if serverRunning || sessionCount != 0 {
		t.Fatalf("failed open retained state: server=%v sessions=%d", serverRunning, sessionCount)
	}
}
