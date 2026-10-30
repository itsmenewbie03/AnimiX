package player

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/itsmenewbie03/animix/internal/catalog"
)

func property(name, data string) ipcMessage {
	return ipcMessage{Event: "property-change", Name: name, Data: json.RawMessage(data)}
}

func TestObservationTracksInitialPlaybackPauseSeekAndEnd(t *testing.T) {
	now := time.Now()
	state := observationState{value: Observation{Rate: 1}}
	state.apply(property("time-pos", "30"), now) // Attaching after file-loaded still works.
	state.apply(property("duration", "300"), now)
	state.apply(property("speed", "2"), now)
	state.apply(property("pause", "true"), now.Add(time.Second))
	if !state.value.Loaded || !state.value.Paused || state.value.Position != 32 {
		t.Fatalf("paused snapshot = %#v", state.value)
	}
	state.apply(property("pause", "false"), now.Add(10*time.Second))
	if state.value.Position != 32 {
		t.Fatal("position advanced while paused")
	}
	state.apply(property("seeking", "true"), now.Add(11*time.Second))
	state.apply(property("time-pos", "100"), now.Add(11*time.Second))
	if !state.value.Buffering || state.value.Position != 100 {
		t.Fatalf("seek snapshot = %#v", state.value)
	}
	state.apply(ipcMessage{Event: "playback-restart"}, now.Add(12*time.Second))
	if state.value.Buffering {
		t.Fatal("playback restart did not end seeking")
	}
	state.apply(ipcMessage{Event: "end-file"}, now.Add(13*time.Second))
	state.apply(property("time-pos", "300"), now.Add(13*time.Second))
	if state.value.Loaded {
		t.Fatal("late position restored an ended file")
	}
	state.apply(property("time-pos", "null"), now.Add(14*time.Second))
	state.apply(ipcMessage{Event: "file-loaded"}, now.Add(15*time.Second))
	state.apply(property("time-pos", "0"), now.Add(15*time.Second))
	if !state.value.Loaded || state.value.Position != 0 {
		t.Fatalf("replacement file = %#v", state.value)
	}
}

func TestRunObservedReportsWhileChildIsRunningAndPreservesFinalProgress(t *testing.T) {
	session, err := MPVSession("unused", catalog.Video{URL: "fixture"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	session.Command = exec.Command(os.Args[0], "-test.run=^TestMPVHelperProcess$", "--", "fixture")
	session.Command.Env = append(os.Environ(), "ANIMIX_TEST_MPV=1", "ANIMIX_TEST_PROGRESS="+session.resultPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshots := make(chan Observation, 32)
	type result struct {
		progress Progress
		err      error
	}
	done := make(chan result, 1)
	go func() {
		progress, err := session.RunObserved(ctx, func(value Observation) {
			select {
			case snapshots <- value:
			default:
			}
		})
		done <- result{progress, err}
	}()
	for {
		select {
		case value := <-snapshots:
			if !value.Loaded || value.Duration != 100 || value.Rate != 2 {
				continue
			}
			select {
			case <-done:
				t.Fatal("observation was only available after process exit")
			default:
			}
			cancel()
			select {
			case got := <-done:
				if got.progress.Position != 75 || got.progress.Duration != 100 || got.progress.Reason != "quit" {
					t.Fatalf("final progress = %#v, error = %v", got.progress, got.err)
				}
				if _, err := os.Stat(session.directory); !os.IsNotExist(err) {
					t.Fatalf("session directory leaked: %v", err)
				}
				return
			case <-time.After(3 * time.Second):
				t.Fatal("canceled player did not finish")
			}
		case got := <-done:
			t.Fatalf("player exited before live observation: %v", got.err)
		case <-ctx.Done():
			t.Fatal("live observer did not report")
		}
	}
}

func TestFailedPlayerLaunchCleansSession(t *testing.T) {
	session, err := MPVSession("animix-nonexistent-player-fixture", catalog.Video{URL: "fixture"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := session.RunObserved(context.Background(), func(Observation) { called = true }); err == nil {
		t.Fatal("missing executable succeeded")
	}
	if called {
		t.Fatal("failed launch reported playback")
	}
	if _, err := os.Stat(session.directory); !os.IsNotExist(err) {
		t.Fatalf("failed launch leaked directory: %v", err)
	}
}

// Runs as a separate process so the test exercises real process lifetime and
// the native Unix socket / Windows named pipe, without requiring mpv in CI.
func TestMPVHelperProcess(t *testing.T) {
	if os.Getenv("ANIMIX_TEST_MPV") != "1" {
		return
	}
	endpoint := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--input-ipc-server=") {
			endpoint = strings.TrimPrefix(arg, "--input-ipc-server=")
		}
	}
	listener, err := listenIPC(endpoint)
	if err != nil {
		os.Exit(2)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(3)
		}
		go func() {
			defer conn.Close()
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				var request struct {
					Command   []any `json:"command"`
					RequestID int   `json:"request_id"`
				}
				if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.Command) == 0 {
					continue
				}
				if request.Command[0] == "quit" {
					_ = os.WriteFile(os.Getenv("ANIMIX_TEST_PROGRESS"), []byte(`{"position":75,"duration":100,"reason":"quit"}`), 0o600)
					os.Exit(0)
				}
				if request.Command[0] == "observe_property" && len(request.Command) == 3 {
					name, _ := request.Command[2].(string)
					value := map[string]any{"time-pos": 30, "duration": 100, "speed": 2, "pause": false, "paused-for-cache": false, "idle-active": false, "seeking": false}[name]
					_ = json.NewEncoder(conn).Encode(map[string]any{"event": "property-change", "name": name, "data": value})
				}
			}
		}()
	}
}
