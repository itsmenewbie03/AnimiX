package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/discord"
)

type DiscordPresence interface {
	Configure(discord.Settings)
	Begin(discord.Snapshot) discord.SessionID
	Update(discord.SessionID, discord.Snapshot)
	End(discord.SessionID)
	Status() discord.Status
}

func WithDiscordPresence(presence DiscordPresence) Option {
	return func(model *Model) { model.discord = presence }
}

func WithAppContext(ctx context.Context) Option {
	return func(model *Model) { model.appContext = ctx }
}

func discordSettings(config appconfig.Config) discord.Settings {
	prefs := config.DiscordPreferences()
	return discord.Settings{
		Enabled: prefs.Enabled, ShowBrowsing: prefs.ShowBrowsing, Token: config.DiscordToken,
		ApplicationID: prefs.ApplicationID, LargeImage: prefs.LargeImage, ShowArtwork: prefs.ShowArtwork,
	}
}

type discordTickMsg struct{}

func discordTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return discordTickMsg{} })
}

func (m *Model) saveDiscord(change func(*appconfig.Config), stopFirst bool) error {
	if stopFirst && m.discord != nil {
		m.discord.Configure(discord.Settings{})
	}
	if m.configStore == nil {
		return fmt.Errorf("Discord settings store is unavailable")
	}
	config, err := m.configStore.Load()
	if err != nil {
		return err
	}
	preferences := config.DiscordPreferences()
	config.Discord = &preferences
	change(&config)
	if err := m.configStore.Save(config); err != nil {
		return fmt.Errorf("save Discord settings: %w", err)
	}
	if m.discord != nil {
		m.discord.Configure(discordSettings(config))
	}
	return nil
}

func (m Model) handleDiscordKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.stage == stageDiscordToken {
		switch key.String() {
		case "esc":
			m.discordInput.SetValue("")
			m.discordInput.Blur()
			m.stage = stageDiscord
			return m, discordTick()
		case "ctrl+u":
			m.discordInput.SetValue("")
			return m, nil
		case "enter":
			token := strings.TrimSpace(m.discordInput.Value())
			if token == "" {
				m.err = fmt.Errorf("Discord user-session token is required")
				return m, nil
			}
			m.err = m.saveDiscord(func(config *appconfig.Config) { config.DiscordToken = token }, false)
			m.discordInput.SetValue("")
			m.discordInput.Blur()
			m.stage = stageDiscord
			return m, discordTick()
		default:
			var command tea.Cmd
			m.discordInput, command = m.discordInput.Update(key)
			return m, command
		}
	}
	if key.String() == "q" {
		return m, tea.Quit
	}
	if m.discord == nil {
		if key.String() == "esc" || key.String() == "backspace" {
			m.stage = stageSources
		}
		return m, nil
	}
	status := m.discord.Status()
	switch key.String() {
	case "esc", "backspace":
		m.stage = stageSources
	case "t":
		m.stage = stageDiscordToken
		m.discordInput.SetValue("")
		m.discordInput.Focus()
		return m, textinput.Blink
	case "e":
		m.err = m.saveDiscord(func(config *appconfig.Config) { config.Discord.Enabled = !status.Enabled }, status.Enabled)
	case "b":
		m.err = m.saveDiscord(func(config *appconfig.Config) { config.Discord.ShowBrowsing = !status.ShowBrowsing }, false)
	case "a":
		m.err = m.saveDiscord(func(config *appconfig.Config) { config.Discord.ShowArtwork = !status.ShowArtwork }, false)
	case "x":
		m.err = m.saveDiscord(func(config *appconfig.Config) {
			config.DiscordToken = ""
			config.Discord.Enabled = false
		}, true)
	case "r":
		if m.configStore != nil {
			config, err := m.configStore.Load()
			m.err = err
			if err == nil {
				m.discord.Configure(discordSettings(config))
			}
		}
	}
	return m, nil
}

func (m Model) discordView() string {
	if m.stage == stageDiscordToken {
		return titleStyle.Render("Discord token") + "\n\n" +
			"Paste a Discord user-session token. This is the unofficial Gateway login,\nnot an OAuth or bot token. It is saved in managed state.json.\nSaving a token does not turn on sharing.\n\n" +
			m.discordInput.View() + "\n\n" + helpStyle.Render("enter save • ctrl+u clear • esc cancel")
	}
	if m.discord == nil {
		return "Discord presence is unavailable.\n\nesc home"
	}
	status := m.discord.Status()
	flag := func(value bool) string {
		if value {
			return "On"
		}
		return "Off"
	}
	credential := "Not set"
	if status.HasToken {
		credential = "Saved"
	}
	var output strings.Builder
	output.WriteString(titleStyle.Render("Discord presence") + "\n\n")
	fmt.Fprintf(&output, "Sharing: %s\nBrowsing: %s\nPoster artwork: %s\nToken: %s\nConnection: %s\n",
		flag(status.Enabled), flag(status.ShowBrowsing), flag(status.ShowArtwork), credential, status.State)
	if status.ShowArtwork && !status.HasApplicationID {
		output.WriteString("Poster artwork needs application_id in config.toml.\n")
	} else if status.ShowArtwork {
		fmt.Fprintf(&output, "Poster status: %s", status.ArtworkState)
		if status.ArtworkHTTPStatus > 0 {
			fmt.Fprintf(&output, " (Discord HTTP %d)", status.ArtworkHTTPStatus)
		}
		output.WriteString("\n")
	}
	if status.Username != "" {
		fmt.Fprintf(&output, "Account: %s\n", status.Username)
	}
	if status.Message != "" {
		output.WriteString("\n" + status.Message + "\n")
	}
	if !status.LastSent.IsZero() {
		fmt.Fprintf(&output, "Last activity sent: %s\n", status.LastSent.Local().Format("15:04:05"))
	}
	if !status.LastACK.IsZero() {
		fmt.Fprintf(&output, "Last heartbeat ACK: %s\n", status.LastACK.Local().Format("15:04:05"))
	}
	if status.CloseCode > 0 {
		fmt.Fprintf(&output, "Last Gateway close code: %d\n", status.CloseCode)
	}
	if status.State == discord.RetryWait {
		fmt.Fprintf(&output, "Retry: %s\n", status.RetryAt.Local().Format("15:04:05"))
	}
	output.WriteString("\nShares episode/chapter information directly with Discord.\nThe Discord desktop app can be closed.\n\n")
	output.WriteString(helpStyle.Render("e sharing on/off • t set token • b browsing on/off\na poster artwork on/off • r retry • x forget token • esc home • q quit"))
	return output.String()
}
