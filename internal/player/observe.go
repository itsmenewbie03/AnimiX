package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"os"
	"time"
)

// Observation is a live mpv snapshot, independent of final watch history.
type Observation struct {
	Position, Duration, Rate float64
	Paused, Buffering        bool
	Loaded                   bool
	ObservedAt               time.Time
}

// RunObserved owns the child and observer together. No goroutine is started by
// MPVSession construction, and observation failures never stop playback.
func (s *Session) RunObserved(ctx context.Context, report func(Observation)) (Progress, error) {
	defer os.RemoveAll(s.directory)
	if err := ctx.Err(); err != nil {
		return Progress{}, err
	}
	endpoint := ""
	if report != nil {
		path, cleanup, err := ipcEndpoint(s.directory)
		if err == nil {
			defer cleanup()
			endpoint = path
			last := len(s.Command.Args) - 1
			args := append([]string(nil), s.Command.Args[:last]...)
			args = append(args, "--input-ipc-server="+endpoint, s.Command.Args[last])
			s.Command.Args = args
		}
	}
	if err := s.Command.Start(); err != nil {
		return Progress{}, err
	}
	observerCtx, stopObserver := context.WithCancel(ctx)
	observerDone := make(chan struct{})
	go func() {
		defer close(observerDone)
		if endpoint != "" {
			observeMPV(observerCtx, endpoint, report)
		}
	}()
	wait := make(chan error, 1)
	go func() { wait <- s.Command.Wait() }()
	var runErr error
	select {
	case runErr = <-wait:
	case <-ctx.Done():
		// Give mpv a bounded chance to flush its Lua progress file. Termination
		// still completes when IPC is unavailable or the child is unresponsive.
		requestPlayerQuit(endpoint)
		select {
		case runErr = <-wait:
		case <-time.After(time.Second):
			_ = s.Command.Process.Kill()
			runErr = <-wait
		}
		runErr = errors.Join(runErr, ctx.Err())
	}
	stopObserver()
	<-observerDone
	progress, progressErr := s.Progress() // Exactly one destructive final read.
	return progress, errors.Join(runErr, progressErr)
}

func requestPlayerQuit(endpoint string) {
	if endpoint == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	conn, err := dialIPC(ctx, endpoint)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	_, _ = conn.Write([]byte("{\"command\":[\"quit\"]}\n"))
}

func observeMPV(ctx context.Context, endpoint string, report func(Observation)) {
	for ctx.Err() == nil {
		dialCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, err := dialIPC(dialCtx, endpoint)
		cancel()
		if err == nil {
			observeConnection(ctx, conn, report)
		}
		if ctx.Err() != nil {
			return
		}
		report(Observation{ObservedAt: time.Now()})
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type ipcMessage struct {
	Event     string          `json:"event"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
}

func observeConnection(ctx context.Context, conn net.Conn, report func(Observation)) {
	readerCtx, stop := context.WithCancel(ctx)
	lines := make(chan ipcMessage, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(lines)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 4096), 512<<10)
		for scanner.Scan() {
			var message ipcMessage
			if json.Unmarshal(scanner.Bytes(), &message) != nil {
				continue
			}
			select {
			case lines <- message:
			case <-readerCtx.Done():
				return
			}
		}
	}()
	defer func() { stop(); conn.Close(); <-done }()
	write := func(command []any, id int) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		return json.NewEncoder(conn).Encode(map[string]any{"command": command, "request_id": id}) == nil
	}
	properties := []string{"time-pos", "duration", "pause", "paused-for-cache", "speed", "idle-active", "seeking"}
	for index, name := range properties {
		if !write([]any{"observe_property", index + 1, name}, index+1) {
			return
		}
	}
	state := observationState{value: Observation{Rate: 1}}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	awaitingRefresh := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if awaitingRefresh || !write([]any{"get_property", "time-pos"}, 1000) {
				return
			}
			awaitingRefresh = true
		case message, ok := <-lines:
			if !ok {
				return
			}
			if message.RequestID == 1000 {
				awaitingRefresh = false
				message.Event, message.Name = "property-change", "time-pos"
			}
			if state.apply(message, time.Now()) {
				report(state.value)
			}
		}
	}
}

type observationState struct {
	value       Observation
	hasPosition bool
	ended       bool
	seeking     bool
	cachePaused bool
}

func (s *observationState) apply(message ipcMessage, now time.Time) bool {
	if message.Event != "property-change" && message.Event != "file-loaded" && message.Event != "end-file" && message.Event != "shutdown" && message.Event != "playback-restart" {
		return false
	}
	if s.value.Loaded && !s.value.Paused && !s.value.Buffering && !s.value.ObservedAt.IsZero() {
		s.value.Position += max(0, now.Sub(s.value.ObservedAt).Seconds()) * s.value.Rate
	}
	s.value.ObservedAt = now
	switch message.Event {
	case "end-file", "shutdown":
		s.ended, s.hasPosition, s.value.Loaded = true, false, false
	case "file-loaded":
		s.ended = false
	case "playback-restart":
		s.seeking = false
	case "property-change":
		switch message.Name {
		case "time-pos":
			position, ok := ipcNumber(message.Data)
			s.hasPosition = ok && position >= 0
			if s.hasPosition {
				s.value.Position = position
			}
		case "duration":
			duration, ok := ipcNumber(message.Data)
			s.value.Duration = 0
			if ok && duration > 0 {
				s.value.Duration = duration
			}
		case "speed":
			if speed, ok := ipcNumber(message.Data); ok && speed > 0 {
				s.value.Rate = speed
			}
		case "pause":
			_ = json.Unmarshal(message.Data, &s.value.Paused)
		case "paused-for-cache":
			_ = json.Unmarshal(message.Data, &s.cachePaused)
		case "seeking":
			_ = json.Unmarshal(message.Data, &s.seeking)
		case "idle-active":
			var idle bool
			if json.Unmarshal(message.Data, &idle) == nil && idle {
				s.hasPosition = false
			}
		}
	}
	s.value.Loaded = s.hasPosition && !s.ended
	s.value.Buffering = s.cachePaused || s.seeking
	return true
}

func ipcNumber(data json.RawMessage) (float64, bool) {
	var number *float64
	if json.Unmarshal(data, &number) != nil || number == nil || math.IsNaN(*number) || math.IsInf(*number, 0) {
		return 0, false
	}
	return *number, true
}
