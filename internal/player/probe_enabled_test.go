//go:build probe

package player

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itsmenewbie03/animix/internal/catalog"
)

func TestPlaybackProbeWritesStructuredEventsAndRedactsSecrets(t *testing.T) {
	t.Setenv("ANIMIX_PROBE_DIR", t.TempDir())
	probe, err := openPlaybackProbe()
	if err != nil {
		t.Fatal(err)
	}
	probe.record("ipc", map[string]any{"name": "time-pos", "data": 496.597333})
	_, _ = probe.stderr().Write([]byte("[ffmpeg] open https://cdn.example/video.m3u8?token=secret Cookie: cf_clearance=secret\n"))
	path := probe.path()
	if err := probe.close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"kind":"ipc"`) || !strings.Contains(text, `496.597333`) {
		t.Fatalf("missing structured IPC event: %s", text)
	}
	if strings.Contains(text, "secret") || strings.Contains(text, "cdn.example") {
		t.Fatalf("probe exposed sensitive stream data: %s", text)
	}
	if !strings.Contains(text, "REDACTED") {
		t.Fatalf("probe did not mark redacted data: %s", text)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("probe permissions = %v, error = %v", info.Mode().Perm(), err)
	}
}

func TestPlaybackProbeCapturesRealSessionLifecycle(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("ANIMIX_PROBE_DIR", directory)
	session, err := MPVSession("unused", catalog.Video{URL: "fixture"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	session.Command = exec.Command(os.Args[0], "-test.run=^TestMPVHelperProcess$", "--", "fixture")
	session.Command.Env = append(os.Environ(), "ANIMIX_TEST_MPV=1", "ANIMIX_TEST_PROGRESS="+session.resultPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = session.RunObserved(ctx, func(value Observation) {
		if value.Loaded {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("canceled fixture session returned no error")
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("probe files = %v, error = %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, kind := range []string{`"kind":"session_start"`, `"kind":"ipc"`, `"kind":"observation"`, `"kind":"session_end"`} {
		if !strings.Contains(text, kind) {
			t.Fatalf("probe missing %s: %s", kind, text)
		}
	}
}
