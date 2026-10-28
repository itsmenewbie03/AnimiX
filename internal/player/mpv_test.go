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
		Title: "BOFURI Season 2 — Maxing Defense and Tentacles. (EP 5)",
		URL:   "https://cdn.example/video.m3u8",
		Headers: map[string]string{
			"User-Agent": "AnymeX",
			"Referer":    "https://example.com/",
		},
		Subtitles: []catalog.Track{{File: "https://cdn.example/sub.vtt"}},
	})
	want := []string{
		"mpv",
		"--force-window=yes",
		"--no-save-position-on-quit",
		"--title=BOFURI Season 2 — Maxing Defense and Tentacles. (EP 5)",
		"--force-media-title=BOFURI Season 2 — Maxing Defense and Tentacles. (EP 5)",
		"--network-timeout=30",
		"--cache=yes",
		"--cache-secs=120",
		"--demuxer-readahead-secs=120",
		"--demuxer-max-bytes=32MiB",
		"--demuxer-lavf-o=http_persistent=0,http_multiple=0,seg_max_retry=5,reconnect=1,reconnect_streamed=1,reconnect_delay_max=5",
		"--http-header-fields=Referer: https://example.com/,User-Agent: AnymeX",
		"--sub-file=https://cdn.example/sub.vtt",
		"https://cdn.example/video.m3u8",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("MPV args = %#v, want %#v", command.Args, want)
	}
}

func TestMPVSessionResumesAtExplicitPosition(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"}, 872.454922)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(session.directory)
	args := session.Command.Args
	want := []string{"--no-resume-playback", "--start=872.455"}
	found := 0
	for _, arg := range args {
		if arg == want[0] {
			found |= 1
		}
		if arg == want[1] {
			found |= 2
		}
	}
	if found != 3 {
		t.Fatalf("args = %#v, want %q and %q", args, want[0], want[1])
	}
}

func TestMPVSessionIgnoresStaleWatchLaterState(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(session.directory)
	for _, arg := range session.Command.Args {
		if arg == "--no-resume-playback" {
			return
		}
		if strings.HasPrefix(arg, "--start=") {
			t.Fatalf("unexpected start argument with position 0: %#v", session.Command.Args)
		}
	}
	t.Fatalf("missing --no-resume-playback: %#v", session.Command.Args)
}

func TestMPVSessionWritesSkipChapters(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"}, 0,
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
	session, err := MPVSession("mpv", catalog.Video{URL: "https://example.test/video.m3u8"}, 0)
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
		`mp.register_event("log-message"`,
		`HTTP error 500`,
		`mp.commandv("quit", "2")`,
		`event.reason`,
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("progress script missing %q:\n%s", want, script)
		}
	}
	if err := os.WriteFile(session.resultPath, []byte(`{"position":42.5,"duration":100,"reason":"eof"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 42.5 || progress.Duration != 100 || progress.Reason != "eof" {
		t.Fatalf("progress = %#v", progress)
	}
	if _, err := os.Stat(session.directory); !os.IsNotExist(err) {
		t.Fatalf("temporary directory still exists: %v", err)
	}
}

func TestMPVSessionReportsProxyFailure(t *testing.T) {
	t.Parallel()
	session, err := MPVSession("mpv", catalog.Video{URL: "http://localhost:1234/m3u8"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.resultPath, []byte(`{"position":61,"duration":100,"reason":"proxy-error","proxyFailure":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if !progress.ProxyFailure || progress.Reason != "proxy-error" {
		t.Fatalf("progress = %#v", progress)
	}
}
