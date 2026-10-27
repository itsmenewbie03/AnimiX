package player

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/itsmenewbie03/animix/internal/catalog"
)

func TestMPVArguments(t *testing.T) {
	t.Parallel()
	command := MPV("mpv", catalog.Video{
		URL: "https://cdn.example/video.m3u8",
		Headers: map[string]string{
			"User-Agent": "AnymeX",
			"Referer":    "https://example.com/",
		},
		Subtitles: []catalog.Track{{File: "https://cdn.example/sub.vtt"}},
	})
	want := []string{
		"mpv",
		"--force-window=yes",
		"--save-position-on-quit=yes",
		"--http-header-fields=Referer: https://example.com/,User-Agent: AnymeX",
		"--sub-file=https://cdn.example/sub.vtt",
		"https://cdn.example/video.m3u8",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("MPV args = %#v, want %#v", command.Args, want)
	}
}

func TestMPVSessionWritesSkipChapters(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"},
		Chapter{Start: 47.365, End: 137.365, Title: "Intro"},
		Chapter{Start: 1342.795, End: 1430.616, Title: "Outro"},
		Chapter{Start: 10, End: 5, Title: "Invalid"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(session.directory)
	chaptersPath := ""
	for _, arg := range session.Command.Args {
		if strings.HasPrefix(arg, "--chapters-file=") {
			chaptersPath = strings.TrimPrefix(arg, "--chapters-file=")
		}
	}
	if chaptersPath == "" {
		t.Fatalf("args = %#v", session.Command.Args)
	}
	content, err := os.ReadFile(chaptersPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"START=47365", "END=137365", "title=Intro", "START=1342795", "title=Outro"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("chapters missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(string(content), "Invalid") {
		t.Fatalf("invalid chapter written:\n%s", content)
	}
}

func TestMPVSessionCapturesProgress(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(session.directory)
	scriptPath := ""
	for _, arg := range session.Command.Args {
		if strings.HasPrefix(arg, "--script=") {
			scriptPath = strings.TrimPrefix(arg, "--script=")
		}
	}
	if scriptPath == "" {
		t.Fatalf("args = %#v", session.Command.Args)
	}
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`mp.observe_property("time-pos"`,
		`mp.observe_property("duration"`,
		`mp.register_event("end-file"`,
		`mp.register_event("shutdown"`,
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("progress script missing %q:\n%s", want, script)
		}
	}
	if err := os.WriteFile(session.resultPath, []byte(`{"position":42.5,"duration":100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 42.5 || progress.Duration != 100 {
		t.Fatalf("progress = %#v", progress)
	}
	if _, err := os.Stat(session.directory); !os.IsNotExist(err) {
		t.Fatalf("temporary directory still exists: %v", err)
	}
}
