package mangareader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Page struct {
	URL     string
	Headers map[string]string
}

type Session struct {
	Title     string
	Chapter   string
	Pages     []Page
	StartPage int
}

type OpenedSession struct {
	ID  string
	URL string
}

type ProgressEvent struct {
	SessionID string
	Page      int
	Total     int
}

type BrowserReader struct {
	openURL func(string) error
	client  *http.Client
	events  chan ProgressEvent

	mutex    sync.RWMutex
	sessions map[string]Session
	listener net.Listener
	server   *http.Server
	baseURL  string
}

func New(openURL func(string) error) *BrowserReader {
	return &BrowserReader{
		openURL:  openURL,
		client:   &http.Client{Timeout: 30 * time.Second},
		events:   make(chan ProgressEvent, 1),
		sessions: make(map[string]Session),
	}
}

func (reader *BrowserReader) Events() <-chan ProgressEvent { return reader.events }

func (reader *BrowserReader) CloseSession(id string) error {
	reader.mutex.Lock()
	delete(reader.sessions, id)
	if len(reader.sessions) > 0 || reader.server == nil {
		reader.mutex.Unlock()
		return nil
	}
	server := reader.server
	listener := reader.listener
	reader.server = nil
	reader.listener = nil
	reader.baseURL = ""
	reader.mutex.Unlock()
	return shutdownServer(server, listener)
}

func (reader *BrowserReader) Open(session Session) (OpenedSession, error) {
	if len(session.Pages) == 0 {
		return OpenedSession{}, fmt.Errorf("open manga reader: chapter has no pages")
	}
	if session.StartPage < 1 || session.StartPage > len(session.Pages) {
		session.StartPage = 1
	}
	id, err := randomID()
	if err != nil {
		return OpenedSession{}, fmt.Errorf("create manga reader session: %w", err)
	}
	if err := reader.start(); err != nil {
		return OpenedSession{}, err
	}
	reader.mutex.Lock()
	reader.sessions[id] = session
	url := reader.baseURL + "/session/" + id
	reader.mutex.Unlock()
	if reader.openURL != nil {
		if err := reader.openURL(url); err != nil {
			if closeErr := reader.CloseSession(id); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
			return OpenedSession{}, fmt.Errorf("open manga reader in browser: %w", err)
		}
	}
	return OpenedSession{ID: id, URL: url}, nil
}

func (reader *BrowserReader) start() error {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	if reader.server != nil {
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start manga reader: %w", err)
	}
	reader.listener = listener
	reader.baseURL = "http://" + listener.Addr().String()
	server := &http.Server{
		Handler:           http.HandlerFunc(reader.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	reader.server = server
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (reader *BrowserReader) Close() error {
	reader.mutex.Lock()
	server := reader.server
	listener := reader.listener
	reader.server = nil
	reader.listener = nil
	reader.baseURL = ""
	reader.sessions = make(map[string]Session)
	reader.mutex.Unlock()
	if server == nil {
		return nil
	}
	return shutdownServer(server, listener)
}

func shutdownServer(server *http.Server, listener net.Listener) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(ctx)
	if listener != nil {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && shutdownErr == nil {
			return closeErr
		}
	}
	return shutdownErr
}

func (reader *BrowserReader) serveHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	if request.URL.Path == "/favicon.ico" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "session" {
		http.NotFound(response, request)
		return
	}
	id := parts[1]
	reader.mutex.RLock()
	session, ok := reader.sessions[id]
	reader.mutex.RUnlock()
	if !ok {
		http.NotFound(response, request)
		return
	}
	switch {
	case len(parts) == 2 && request.Method == http.MethodGet:
		reader.render(response, id, session)
	case len(parts) == 4 && parts[2] == "page" && request.Method == http.MethodGet:
		reader.proxyPage(response, request, session, parts[3])
	case len(parts) == 3 && parts[2] == "progress" && request.Method == http.MethodPost:
		reader.recordProgress(response, request, id, len(session.Pages))
	default:
		http.NotFound(response, request)
	}
}

func (reader *BrowserReader) render(response http.ResponseWriter, id string, session Session) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
	data := struct {
		Session
		ID      string
		Indexes []int
	}{Session: session, ID: id, Indexes: make([]int, len(session.Pages))}
	for index := range data.Indexes {
		data.Indexes[index] = index + 1
	}
	if err := readerTemplate.Execute(response, data); err != nil {
		http.Error(response, "render manga reader", http.StatusInternalServerError)
	}
}

func (reader *BrowserReader) proxyPage(response http.ResponseWriter, request *http.Request, session Session, rawIndex string) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 || index >= len(session.Pages) {
		http.NotFound(response, request)
		return
	}
	page := session.Pages[index]
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, page.URL, nil)
	if err != nil {
		http.Error(response, "invalid page URL", http.StatusBadGateway)
		return
	}
	for key, value := range page.Headers {
		upstreamRequest.Header.Set(key, value)
	}
	upstreamResponse, err := reader.client.Do(upstreamRequest)
	if err != nil {
		http.Error(response, "load manga page", http.StatusBadGateway)
		return
	}
	defer upstreamResponse.Body.Close()
	if upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		http.Error(response, "manga source rejected page", http.StatusBadGateway)
		return
	}
	if contentType := upstreamResponse.Header.Get("Content-Type"); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	response.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(response, upstreamResponse.Body)
}

func (reader *BrowserReader) recordProgress(response http.ResponseWriter, request *http.Request, id string, total int) {
	defer request.Body.Close()
	var payload struct {
		Page int `json:"page"`
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1024))
	if err := decoder.Decode(&payload); err != nil || payload.Page < 1 || payload.Page > total {
		http.Error(response, "invalid page", http.StatusBadRequest)
		return
	}
	reader.mutex.Lock()
	session, ok := reader.sessions[id]
	if !ok {
		reader.mutex.Unlock()
		http.NotFound(response, request)
		return
	}
	session.StartPage = payload.Page
	reader.sessions[id] = session
	reader.mutex.Unlock()
	event := ProgressEvent{SessionID: id, Page: payload.Page, Total: total}
	select {
	case reader.events <- event:
	default:
		select {
		case <-reader.events:
		default:
		}
		select {
		case reader.events <- event:
		default:
		}
	}
	response.WriteHeader(http.StatusNoContent)
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

var readerTemplate = template.Must(template.New("reader").Funcs(template.FuncMap{"minus": func(left, right int) int { return left - right }}).Parse(`<!doctype html>
<html lang="en" data-start-page="{{.StartPage}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — {{.Chapter}}</title>
<style>
:root { color-scheme: dark; background: #09090b; color: #f4f4f5; font-family: system-ui, sans-serif; overflow-anchor: none; }
* { box-sizing: border-box; }
body { margin: 0; background: #09090b; }
header { position: fixed; z-index: 2; top: 12px; left: 50%; transform: translateX(-50%); display: flex; gap: 12px; align-items: center; max-width: calc(100vw - 24px); padding: 8px 14px; border-radius: 999px; background: rgba(24,24,27,.86); backdrop-filter: blur(10px); box-shadow: 0 6px 24px rgba(0,0,0,.35); transition: opacity .18s ease, transform .18s ease; }
header.controls-hidden { opacity: 0; pointer-events: none; transform: translate(-50%, -140%); }
header strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#counter { flex: none; color: #c4b5fd; }
main { width: min(100%, 1100px); margin: 0 auto; padding: 72px 0 40px; }
.page { display: block; width: 100%; height: auto; aspect-ratio: 2 / 3; object-fit: contain; background: #18181b; }
.page + .page { margin-top: 2px; }
@media (min-width: 1150px) { main { padding-left: 12px; padding-right: 12px; } .page { border-radius: 4px; } }
</style>
</head>
<body>
<header id="controls"><strong>{{.Title}} · {{.Chapter}}</strong><span id="counter">{{.StartPage}}/{{len .Pages}}</span></header>
<main>{{range $index := .Indexes}}<img id="page-{{$index}}" class="page" data-page="{{$index}}" loading="lazy" src="/session/{{$.ID}}/page/{{minus $index 1}}" alt="Page {{$index}}">{{end}}</main>
<script>
const total = {{len .Pages}};
const start = {{.StartPage}};
history.scrollRestoration = 'manual';
const pages = Array.from(document.querySelectorAll('.page'));
const startPage = pages[start - 1];
let current = 0;
let ready = false;
let anchor = startPage;
let anchorTop = 0;
let lastScrollY = window.scrollY;
const controls = document.getElementById('controls');
function showControls() { controls.classList.remove('controls-hidden'); }
function hideControls() { controls.classList.add('controls-hidden'); }
function report(page) {
  if (page === current) return;
  current = page;
  document.getElementById('counter').textContent = page + '/' + total;
  fetch('/session/{{.ID}}/progress', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({page}), keepalive:true}).catch(() => {});
}

// Keep the same page at the same viewport offset when an image above it grows.
// Remember our own scroll changes so they cannot become reading progress.
function keepReadingPosition() {
  const delta = anchor.getBoundingClientRect().top - anchorTop;
  if (Math.abs(delta) > .5) window.scrollBy({top:delta, behavior:'instant'});
  anchorTop = anchor.getBoundingClientRect().top;
  lastScrollY = window.scrollY;
}

function rememberReadingPosition() {
  const readingLine = Math.min(64, innerHeight / 4);
  const atBottom = window.scrollY + innerHeight >= document.documentElement.scrollHeight - 1;
  anchor = atBottom ? pages[pages.length - 1] :
    pages.find(page => page.getBoundingClientRect().bottom > readingLine) || pages[pages.length - 1];
  anchorTop = anchor.getBoundingClientRect().top;
  if (ready) report(Number(anchor.dataset.page));
}

function onScroll() {
  const nextScrollY = window.scrollY;
  if (Math.abs(nextScrollY - lastScrollY) < .5) return;
  if (nextScrollY > lastScrollY + 6 && nextScrollY > 48) hideControls();
  if (nextScrollY < lastScrollY - 6) showControls();
  lastScrollY = nextScrollY;
  rememberReadingPosition();
}

// An explicit aspect ratio reserves space even before a lazy image has metadata.
// Applying its real dimensions is the only image-driven reflow, so compensate
// synchronously before it can move the reader's visible page.
function sizePage(page) {
  if (page.naturalWidth > 0 && page.naturalHeight > 0) {
    // A wheel/keyboard scroll may have happened before its scroll event arrived.
    onScroll();
    anchorTop = anchor.getBoundingClientRect().top;
    page.style.aspectRatio = page.naturalWidth + ' / ' + page.naturalHeight;
    keepReadingPosition();
  }
}
startPage.loading = 'eager';
keepReadingPosition();
pages.forEach(page => {
  page.addEventListener('load', () => sizePage(page), {once:true});
  if (page.complete) sizePage(page);
});

// ResizeObserver also preserves the anchor when the browser width changes.
const observer = new ResizeObserver(() => {
  const pendingScroll = window.scrollY - lastScrollY;
  anchorTop -= pendingScroll;
  keepReadingPosition();
  if (Math.abs(pendingScroll) >= .5) rememberReadingPosition();
});
window.addEventListener('load', async () => {
  await startPage.decode().catch(() => {});
  await new Promise(resolve => requestAnimationFrame(resolve));
  onScroll();
  keepReadingPosition();
  ready = true;
  report(Number(anchor.dataset.page));
  pages.forEach(page => observer.observe(page));
});
window.addEventListener('scroll', onScroll, {passive:true});
window.addEventListener('pointermove', event => { if (event.clientY < 96) showControls(); }, {passive:true});
window.addEventListener('keydown', event => {
  if (['ArrowDown','ArrowRight','j',' ','PageDown'].includes(event.key)) { event.preventDefault(); window.scrollBy({top:innerHeight*.85,behavior:'smooth'}); }
  if (['ArrowUp','ArrowLeft','k','PageUp'].includes(event.key)) { event.preventDefault(); window.scrollBy({top:-innerHeight*.85,behavior:'smooth'}); }
  if (event.key === 'Home') { event.preventDefault(); document.getElementById('page-1').scrollIntoView(); }
  if (event.key === 'End') { event.preventDefault(); document.getElementById('page-' + total).scrollIntoView(); }
});
</script>
</body>
</html>`))
