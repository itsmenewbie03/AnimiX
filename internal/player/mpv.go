package player

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/itsmenewbie03/animix/internal/catalog"
)

type Progress struct {
	Position     float64 `json:"position"`
	Duration     float64 `json:"duration"`
	Reason       string  `json:"reason"`
	ProxyFailure bool    `json:"proxyFailure,omitempty"`
}

// networkArguments avoid stale keep-alive sockets and reconnect transient
// network failures. Some extension-local proxies (notably AnimePahe's
// NanoHTTPD server) close idle HTTP connections after five seconds, while HLS
// segments are often longer than that. Reusing a closed connection makes mpv
// stop at a segment boundary with "End of file".
var networkArguments = []string{
	"--network-timeout=30",
	"--cache=yes",
	"--cache-secs=120",
	"--demuxer-readahead-secs=120",
	"--demuxer-max-bytes=32MiB",
	"--demuxer-lavf-o=http_persistent=0,http_multiple=0,seg_max_retry=5,reconnect=1,reconnect_streamed=1,reconnect_delay_max=5",
}

type Session struct {
	Command    *exec.Cmd
	directory  string
	resultPath string
}

// Chapter is a marked range passed to mpv as an OGM/FFmetadata chapter.
type Chapter struct {
	Start float64
	End   float64
	Title string
}

func MPV(executable string, video catalog.Video) *exec.Cmd {
	args := []string{"--force-window=yes", "--no-save-position-on-quit"}
	args = append(args, networkArguments...)
	if logPath := os.Getenv("ANIMIX_MPV_LOG"); logPath != "" {
		args = append(args, "--log-file="+logPath)
	}
	if len(video.Headers) > 0 {
		keys := make([]string, 0, len(video.Headers))
		for key := range video.Headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		headers := make([]string, 0, len(keys))
		for _, key := range keys {
			headers = append(headers, fmt.Sprintf("%s: %s", key, video.Headers[key]))
		}
		args = append(args, "--http-header-fields="+strings.Join(headers, ","))
	}
	for _, subtitle := range video.Subtitles {
		if subtitle.File != "" {
			args = append(args, "--sub-file="+subtitle.File)
		}
	}
	args = append(args, video.URL)
	return exec.Command(executable, args...)
}

func MPVSession(executable string, video catalog.Video, start float64, chapters ...Chapter) (*Session, error) {
	directory, err := os.MkdirTemp("", "anymex-mpv-")
	if err != nil {
		return nil, fmt.Errorf("create mpv progress directory: %w", err)
	}
	resultPath := filepath.Join(directory, "progress.json")
	scriptPath := filepath.Join(directory, "capture-progress.lua")
	script := `local output = ` + strconv.Quote(resultPath) + `
local position, duration = 0, 0
local reason = "unknown"
local proxy_failure = false
local proxy_errors = 0
local function snapshot()
  local currentPosition = mp.get_property_number("time-pos")
  local currentDuration = mp.get_property_number("duration")
  if currentPosition then position = currentPosition end
  if currentDuration and currentDuration > 0 then duration = currentDuration end
end
mp.observe_property("time-pos", "number", function(_, value)
  if value then position = value end
end)
mp.observe_property("duration", "number", function(_, value)
  if value and value > 0 then duration = value end
end)
local function write_progress()
  snapshot()
  local file = io.open(output, "w")
  if file then
    file:write(string.format('{"position":%.6f,"duration":%.6f,"reason":"%s","proxyFailure":%s}', position, duration, reason, tostring(proxy_failure)))
    file:close()
  end
end
mp.enable_messages("warn")
mp.register_event("log-message", function(event)
  local text = event.text or ""
  if text:find("HTTP error 500", 1, true) or text:find("Server returned 5XX", 1, true) then
    proxy_errors = proxy_errors + 1
    if proxy_errors >= 3 then
      proxy_failure = true
      reason = "proxy-error"
      write_progress()
      mp.commandv("quit", "2")
    end
  end
end)
mp.register_event("end-file", function(event)
  if not proxy_failure then reason = event.reason or "unknown" end
  write_progress()
end)
mp.register_event("shutdown", write_progress)
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		os.RemoveAll(directory)
		return nil, fmt.Errorf("write mpv progress script: %w", err)
	}
	extra := []string{"--no-resume-playback"}
	if start > 0 {
		extra = append(extra, fmt.Sprintf("--start=%.3f", start))
	}
	if chapterPath, chapterErr := writeChapters(directory, chapters); chapterErr != nil {
		os.RemoveAll(directory)
		return nil, chapterErr
	} else if chapterPath != "" {
		extra = append(extra, "--chapters-file="+chapterPath)
	}
	extra = append(extra, "--script="+scriptPath)
	command := MPV(executable, video)
	last := len(command.Args) - 1
	args := make([]string, 0, len(command.Args)+len(extra))
	args = append(args, command.Args[:last]...)
	args = append(args, extra...)
	args = append(args, command.Args[last])
	command.Args = args
	return &Session{Command: command, directory: directory, resultPath: resultPath}, nil
}

func writeChapters(directory string, chapters []Chapter) (string, error) {
	valid := make([]Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter.Start >= 0 && chapter.End >= chapter.Start && strings.TrimSpace(chapter.Title) != "" {
			valid = append(valid, chapter)
		}
	}
	if len(valid) == 0 {
		return "", nil
	}
	var content strings.Builder
	content.WriteString(";FFMETADATA1\n")
	for _, chapter := range valid {
		fmt.Fprintf(&content, "[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n",
			int(math.Round(chapter.Start*1000)), int(math.Round(chapter.End*1000)), chapter.Title)
	}
	path := filepath.Join(directory, "chapters.txt")
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		return "", fmt.Errorf("write mpv chapters: %w", err)
	}
	return path, nil
}

func (s *Session) Progress() (Progress, error) {
	defer os.RemoveAll(s.directory)
	data, err := os.ReadFile(s.resultPath)
	if err != nil {
		return Progress{}, err
	}
	var progress Progress
	if err := json.Unmarshal(data, &progress); err != nil {
		return Progress{}, err
	}
	return progress, nil
}
