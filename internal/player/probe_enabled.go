//go:build probe

package player

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	probeURLPattern       = regexp.MustCompile(`(?i)https?://[^\s"']+`)
	probeSensitivePattern = regexp.MustCompile(`(?i)(cookie|authorization|proxy-authorization|http-header-fields)\s*[:=]\s*.*`)
)

type filePlaybackProbe struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	lines   *probeLineWriter
	closed  bool
}

func openPlaybackProbe() (playbackProbe, error) {
	directory := strings.TrimSpace(os.Getenv("ANIMIX_PROBE_DIR"))
	if directory == "" {
		directory = os.Getenv("XDG_STATE_HOME")
		if directory == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			directory = filepath.Join(home, ".local", "state")
		}
		directory = filepath.Join(directory, "animix", "probes")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	name := "playback-" + time.Now().Format("20060102-150405.000000000") + ".jsonl"
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	probe := &filePlaybackProbe{file: file, encoder: json.NewEncoder(file)}
	probe.lines = &probeLineWriter{probe: probe}
	probe.record("probe_start", map[string]any{"format": 1})
	return probe, nil
}

func (p *filePlaybackProbe) record(kind string, fields map[string]any) {
	event := make(map[string]any, len(fields)+2)
	event["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	event["kind"] = kind
	for key, value := range fields {
		event[key] = value
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		_ = p.encoder.Encode(event)
	}
}

func (p *filePlaybackProbe) stderr() io.Writer { return p.lines }
func (p *filePlaybackProbe) path() string      { return p.file.Name() }

func (p *filePlaybackProbe) close() error {
	p.lines.flush()
	p.record("probe_end", nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.file.Close()
}

type probeLineWriter struct {
	mu     sync.Mutex
	probe  *filePlaybackProbe
	buffer []byte
}

func (w *probeLineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.buffer = append(w.buffer, data...)
	var lines []string
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		lines = append(lines, string(w.buffer[:index]))
		w.buffer = w.buffer[index+1:]
	}
	w.mu.Unlock()
	for _, line := range lines {
		w.probe.record("mpv_stderr", map[string]any{"message": redactProbeLine(line)})
	}
	return len(data), nil
}

func (w *probeLineWriter) flush() {
	w.mu.Lock()
	line := string(w.buffer)
	w.buffer = nil
	w.mu.Unlock()
	if line != "" {
		w.probe.record("mpv_stderr", map[string]any{"message": redactProbeLine(line)})
	}
}

func redactProbeLine(line string) string {
	line = probeURLPattern.ReplaceAllString(line, "<REDACTED>")
	return probeSensitivePattern.ReplaceAllString(line, "$1: <REDACTED>")
}
