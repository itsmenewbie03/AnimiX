package tui

import (
	"context"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/discord"
	"github.com/itsmenewbie03/animix/internal/player"
)

// observedPlayback implements tea.ExecCommand without relying on TUI ticks or
// Program.Send during playback. All resources are created inside Run so a
// canceled/undelivered Exec command cannot leave an observer or temporary files.
type observedPlayback struct {
	ctx        context.Context
	executable string
	video      catalog.Video
	entry      appconfig.ContinueEntry
	chapters   []player.Chapter
	presence   DiscordPresence
	metadata   discord.Snapshot
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	progress   *player.Progress
}

var _ tea.ExecCommand = (*observedPlayback)(nil)

func (p *observedPlayback) SetStdin(input io.Reader)   { p.stdin = input }
func (p *observedPlayback) SetStdout(output io.Writer) { p.stdout = output }
func (p *observedPlayback) SetStderr(output io.Writer) { p.stderr = output }

func (p *observedPlayback) Run() error {
	session, err := player.MPVSession(p.executable, p.video, p.entry.Position, p.chapters...)
	if err != nil {
		return err
	}
	session.Command.Stdin, session.Command.Stdout, session.Command.Stderr = p.stdin, p.stdout, p.stderr
	var report func(player.Observation)
	if p.presence != nil {
		id := p.presence.Begin(p.metadata)
		defer p.presence.End(id)
		report = func(observation player.Observation) {
			snapshot := p.metadata
			snapshot.Active = observation.Loaded
			snapshot.Position, snapshot.Duration, snapshot.Rate = observation.Position, observation.Duration, observation.Rate
			snapshot.Paused, snapshot.Buffering = observation.Paused, observation.Buffering
			snapshot.ObservedAt, snapshot.ValidUntil = observation.ObservedAt, observation.ObservedAt.Add(15*time.Second)
			p.presence.Update(id, snapshot)
		}
	}
	progress, err := session.RunObserved(p.ctx, report)
	p.progress = &progress
	return err
}
