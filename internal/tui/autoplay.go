package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/notification"
	"github.com/itsmenewbie03/animix/internal/player"
)

type autoPlayPhase uint8

const (
	autoPlayIdle autoPlayPhase = iota
	autoPlayLoadingDetails
	autoPlayPrompting
	autoPlayLoadingStreams
	autoPlayChoosingStream
)

type autoPlayState struct {
	phase           autoPlayPhase
	completed       appconfig.ContinueEntry
	next            catalog.Episode
	nextIndex       int
	preferredStream string
}

var autoPlayChoices = []string{"No", "Yes", "Always", "Never"}

type nextEpisodeOutcome uint8

const (
	nextEpisodeUnknown nextEpisodeOutcome = iota
	nextEpisodeFound
	nextEpisodeUnavailable
)

func shouldOfferAutoPlay(runErr error, progress player.Progress) bool {
	return runErr == nil && !progress.ProxyFailure && progress.Duration > 0 &&
		progress.Reason == "eof" && progress.Position/progress.Duration >= watchedThreshold
}

// nextEpisodeAfter chooses by episode number rather than provider order because
// extensions may return episode lists newest-first or oldest-first.
func nextEpisodeAfter(episodes []catalog.Episode, completed string) (catalog.Episode, int, nextEpisodeOutcome) {
	current, err := strconv.ParseFloat(normalizedEpisodeNumber(completed), 64)
	if err != nil || math.IsNaN(current) || math.IsInf(current, 0) {
		return catalog.Episode{}, 0, nextEpisodeUnknown
	}
	var next catalog.Episode
	nextIndex := 0
	nextNumber := 0.0
	found := false
	for index, episode := range episodes {
		number, parseErr := strconv.ParseFloat(normalizedEpisodeNumber(episode.Number()), 64)
		if parseErr != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= current {
			continue
		}
		if !found || number < nextNumber {
			next, nextIndex, nextNumber, found = episode, index, number, true
		}
	}
	if !found {
		return catalog.Episode{}, 0, nextEpisodeUnavailable
	}
	return next, nextIndex, nextEpisodeFound
}

func (m Model) beginAutoPlay(entry appconfig.ContinueEntry) (tea.Model, tea.Cmd) {
	m.autoPlay = autoPlayState{
		completed:       entry,
		preferredStream: strings.TrimSpace(firstNonEmpty(entry.Quality, entry.VideoURL)),
	}
	if len(m.details.Episodes) == 0 {
		m.autoPlay.phase = autoPlayLoadingDetails
		m.stage, m.cursor = stageAutoPlayPrompt, 0
		m.loading, m.err, m.status = true, nil, ""
		return m, tea.Batch(m.spinner.Tick, m.loadDetails())
	}
	return m.resolveAutoPlayNext()
}

func (m Model) resolveAutoPlayNext() (tea.Model, tea.Cmd) {
	next, index, outcome := nextEpisodeAfter(m.details.Episodes, m.autoPlay.completed.EpisodeNumber)
	switch outcome {
	case nextEpisodeUnknown:
		return m.finishAutoPlay(
			"Autoplay stopped",
			"Could not determine the next episode.",
			notification.Warning,
		)
	case nextEpisodeUnavailable:
		return m.finishAutoPlay(
			"Caught up",
			firstNonEmpty(m.autoPlay.completed.Title, "No later episode is available."),
			notification.Info,
		)
	}

	m.setEpisodeItems()
	m.autoPlay.next, m.autoPlay.nextIndex = next, index
	if m.autoPlayNextEpisode == nil {
		m.autoPlay.phase = autoPlayPrompting
		m.stage, m.cursor = stageAutoPlayPrompt, 0
		m.loading, m.err, m.status = false, nil, ""
		return m, nil
	}
	if !*m.autoPlayNextEpisode {
		return m.finishAutoPlay(
			"Playback finished",
			continueEpisodeLabel(m.autoPlay.completed.EpisodeNumber, m.autoPlay.completed.EpisodeName),
			notification.Info,
		)
	}
	return m.startAutoPlayNext()
}

func (m Model) startAutoPlayNext() (tea.Model, tea.Cmd) {
	m.selectedEpisode = m.autoPlay.next
	m.setEpisodeItems()
	m.episodeList.Select(m.autoPlay.nextIndex)
	m.cursor = m.autoPlay.nextIndex
	m.autoPlay.phase = autoPlayLoadingStreams
	m.stage = stageEpisodes
	m.loading, m.err, m.status = true, nil, ""
	return m, tea.Batch(m.spinner.Tick, m.loadVideos())
}

func (m Model) handleAutoPlayVideos(videos []catalog.Video) (tea.Model, tea.Cmd) {
	if len(videos) == 0 {
		m.err = fmt.Errorf("no streams found for %s", episodePrimaryLabel(m.autoPlay.next))
		m.autoPlay = autoPlayState{}
		m.finishPlaybackNavigation()
		return m, nil
	}
	m.stage = stageStreams
	m.rememberStream = m.hasStreamPreference()
	if index, ok := m.autoPlayVideoIndex(videos); ok {
		m.cursor = index
		return m.playSelected()
	}
	m.cursor = 0
	m.autoPlay.phase = autoPlayChoosingStream
	m.status = "Choose a stream for " + episodePrimaryLabel(m.autoPlay.next) + " to continue"
	return m, nil
}

func (m Model) autoPlayVideoIndex(videos []catalog.Video) (int, bool) {
	if preferred := m.autoPlay.preferredStream; preferred != "" {
		for index, video := range videos {
			if streamMatches(video, preferred) {
				return index, true
			}
		}
	}
	return m.automaticVideoIndex(videos)
}

func (m Model) handleAutoPlayPromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		return m.finishAutoPlay(
			"Playback finished",
			continueEpisodeLabel(m.autoPlay.completed.EpisodeNumber, m.autoPlay.completed.EpisodeName),
			notification.Info,
		)
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(autoPlayChoices)-1, m.cursor+1)
	case "enter":
		return m.activateAutoPlayChoice()
	}
	return m, nil
}

func (m Model) activateAutoPlayChoice() (tea.Model, tea.Cmd) {
	switch m.cursor {
	case 0: // No: stop once and remain in ask mode.
		return m.finishAutoPlay(
			"Playback finished",
			continueEpisodeLabel(m.autoPlay.completed.EpisodeNumber, m.autoPlay.completed.EpisodeName),
			notification.Info,
		)
	case 1: // Yes: play once and ask again after this episode.
		return m.startAutoPlayNext()
	case 2: // Always.
		if err := m.saveAutoPlaySetting(true); err != nil {
			m.err = err
			return m, nil
		}
		return m.startAutoPlayNext()
	case 3: // Never.
		if err := m.saveAutoPlaySetting(false); err != nil {
			m.err = err
			return m, nil
		}
		return m.finishAutoPlay(
			"Autoplay disabled",
			"Future episodes will not prompt.",
			notification.Info,
		)
	}
	return m, nil
}

func (m *Model) saveAutoPlaySetting(enabled bool) error {
	if m.configStore != nil {
		if err := m.configStore.SetAutoPlayNextEpisode(enabled); err != nil {
			return fmt.Errorf("save autoplay setting: %w", err)
		}
	}
	value := enabled
	m.autoPlayNextEpisode = &value
	return nil
}

func (m Model) finishAutoPlay(title, message string, level notification.Level) (tea.Model, tea.Cmd) {
	m.autoPlay = autoPlayState{}
	m.loading, m.status = false, ""
	m.finishPlaybackNavigation()
	return m, m.notifier.Notify(notification.Notice{Title: title, Message: message, Level: level})
}

func (m *Model) finishPlaybackNavigation() {
	if m.playbackReturnHome {
		m.stage = stageSources
		m.homeFocus = homeFocusResumeWatching
		m.cursor = 0
		m.playbackReturnHome = false
		return
	}
	if len(m.details.Episodes) > 0 {
		m.stage = stageEpisodes
		m.cursor = m.episodeList.Index()
	}
}
