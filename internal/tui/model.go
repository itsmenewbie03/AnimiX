package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/aniskip"
	"github.com/itsmenewbie03/animix/internal/browser"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/player"
	"github.com/itsmenewbie03/animix/internal/repository"
)

// New runtimes report challenges immediately. Keep a bounded fallback for old
// runtimes and extensions that hide or transform the underlying HTTP response.
const extensionRequestTimeout = 10 * time.Second

type stage uint8

const (
	stageSources stage = iota
	stageSearch
	stageResults
	stageDetails
	stageEpisodes
	stageStreams
	stagePages
	stageRepositories
	stageRepositoryAdd
	stageRepositoryType
	stageAvailableExtensions
	stageExtensionSearch
	stageAniListToken
	stageAniListLibrary
	stageAniListEdit
	stageEpisodeSource
	stageAlternativeTitle
	stageDiscoverSources
	stageCloudflareCookie
	stageCloudflareUserAgent
)

type cloudflareOperation uint8

const (
	cloudflareNone cloudflareOperation = iota
	cloudflareSearch
	cloudflareDetails
	cloudflareVideos
	cloudflarePages
)

type homeFocus uint8

const (
	homeFocusNone homeFocus = iota
	homeFocusResumeWatching
	homeFocusNewEpisodes
	homeFocusContinueWatching
)

type Model struct {
	service              *catalog.Service
	configStore          *appconfig.Store
	repositoryClient     *repository.Client
	installer            *repository.Installer
	anilistClient        *anilist.Client
	aniskipClient        *aniskip.Client
	extensionDirectory   string
	player               string
	stage                stage
	homeType             string
	homeFocus            homeFocus
	width                int
	height               int
	cursor               int
	loading              bool
	err                  error
	status               string
	spinner              spinner.Model
	search               textinput.Model
	repositoryInput      textinput.Model
	extensionSearch      textinput.Model
	tokenInput           textinput.Model
	alternativeTitle     textinput.Model
	cookieInput          textinput.Model
	userAgentInput       textinput.Model
	sources              []catalog.Source
	results              []catalog.Media
	details              catalog.Media
	videos               []catalog.Video
	pages                []catalog.Page
	selectedSource       catalog.Source
	selectedMedia        catalog.Media
	selectedEpisode      catalog.Episode
	repositories         []appconfig.Repository
	availableExtensions  []repository.Extension
	selectedRepository   appconfig.Repository
	pendingRepositoryURL string
	extensionQuery       string
	extensionLanguage    string
	anilistToken         string
	viewer               anilist.Viewer
	trackingEntries      []anilist.Entry
	continueWatching     []appconfig.ContinueEntry
	activeSources        map[string]string
	mediaMappings        []appconfig.MediaMapping
	sourceCredentials    []appconfig.SourceCredential
	streamPreferences    []appconfig.StreamPreference
	newEpisodesList      list.Model
	resumeWatchingList   list.Model
	continueWatchingList list.Model
	episodeList          list.Model
	editingEntry         anilist.Entry
	resolvingEpisode     *anilist.Entry
	manualMapping        bool
	resolutionQuery      string
	cloudflareOperation  cloudflareOperation
	cloudflareReturn     stage
	rememberStream       bool
	malID                int
}

type newEpisodeListItem struct {
	entry       anilist.Entry
	releaseDate *time.Time
}

func (item newEpisodeListItem) Title() string { return item.entry.Title }

func (item newEpisodeListItem) Description() string {
	description := fmt.Sprintf("EP %d • %d behind • Watched %d of %d",
		item.entry.Available, item.entry.Available-item.entry.Progress,
		item.entry.Progress, item.entry.Available)
	if label := releaseDateLabel(item.releaseDate, time.Now()); label != "" {
		description += " • " + label
	}
	return description
}

func (item newEpisodeListItem) FilterValue() string { return item.entry.Title }

type continueWatchingListItem struct {
	entry anilist.Entry
}

func (item continueWatchingListItem) Title() string { return item.entry.Title }

func (item continueWatchingListItem) Description() string {
	total := "?"
	if item.entry.Total > 0 {
		total = fmt.Sprint(item.entry.Total)
	}
	return fmt.Sprintf("Watching  •  Progress %d/%s", item.entry.Progress, total)
}

func (item continueWatchingListItem) FilterValue() string { return item.entry.Title }

type resumeWatchingListItem struct {
	entry appconfig.ContinueEntry
}

func (item resumeWatchingListItem) Title() string { return item.entry.Title }

func (item resumeWatchingListItem) Description() string {
	description := continueEpisodeLabel(item.entry.EpisodeNumber, item.entry.EpisodeName)
	if item.entry.Duration > 0 {
		description += fmt.Sprintf("  •  %.0f%%", item.entry.Position/item.entry.Duration*100)
	}
	return description
}

func (item resumeWatchingListItem) FilterValue() string { return item.entry.Title }

type episodeListItem struct {
	episode        catalog.Episode
	seriesTitle    string
	aniListWatched bool
	localWatched   bool
}

func (item episodeListItem) Title() string {
	return episodePrimaryLabel(item.episode)
}

func (item episodeListItem) Description() string {
	return episodeSubtitle(item.episode, item.seriesTitle)
}

func (item episodeListItem) FilterValue() string { return item.Title() }

type sourcesMsg struct {
	items []catalog.Source
	err   error
}

type searchMsg struct {
	pages catalog.Pages
	err   error
}

type detailsMsg struct {
	media catalog.Media
	malID int
	err   error
}

type videosMsg struct {
	items []catalog.Video
	err   error
}

type pagesMsg struct {
	items []catalog.Page
	err   error
}

type playerFinishedMsg struct {
	err      error
	progress *player.Progress
	entry    *appconfig.ContinueEntry
}

type repositoriesMsg struct {
	items             []appconfig.Repository
	token             string
	continueWatching  []appconfig.ContinueEntry
	activeSources     map[string]string
	mediaMappings     []appconfig.MediaMapping
	sourceCredentials []appconfig.SourceCredential
	streamPreferences []appconfig.StreamPreference
	err               error
}

type availableExtensionsMsg struct {
	items []repository.Extension
	err   error
}

type (
	repositorySavedMsg    struct{ err error }
	repositoryRemovedMsg  struct{ err error }
	extensionInstalledMsg struct {
		path string
		err  error
	}
	extensionRemovedMsg struct {
		path string
		err  error
	}
)

type aniListLibraryMsg struct {
	library    anilist.Library
	token      string
	open       bool
	background bool
	err        error
}

type aniListUpdatedMsg struct {
	entry anilist.Entry
	err   error
}

type aniListPlaybackUpdatedMsg struct {
	entry    anilist.Entry
	playback appconfig.ContinueEntry
	err      error
}

type (
	aniListLogoutMsg     struct{ err error }
	browserOpenedMsg     struct{ err error }
	credentialsLoadedMsg struct{ err error }
	cloudflareAppliedMsg struct {
		credential appconfig.SourceCredential
		err        error
	}
)

var (
	accent     = lipgloss.Color("#9B87F5")
	dim        = lipgloss.Color("#777777")
	errorColor = lipgloss.Color("#FF6B6B")
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selected   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	helpStyle  = lipgloss.NewStyle().Foreground(dim)
	errorStyle = lipgloss.NewStyle().Foreground(errorColor)
)

func New(service *catalog.Service, store *appconfig.Store, extensionDirectory, playerExecutable string) Model {
	input := textinput.New()
	input.Placeholder = "Search anime or manga"
	input.CharLimit = 200
	input.Width = 50
	repoInput := textinput.New()
	repoInput.Placeholder = "https://example.org/repo/index.min.json"
	repoInput.CharLimit = 2048
	repoInput.Width = 70
	extSearch := textinput.New()
	extSearch.Placeholder = "Search extensions"
	extSearch.CharLimit = 200
	extSearch.Width = 50
	tokenInput := textinput.New()
	tokenInput.Placeholder = "AniList access token"
	tokenInput.CharLimit = 4096
	tokenInput.Width = 70
	tokenInput.EchoMode = textinput.EchoPassword
	tokenInput.EchoCharacter = '•'
	altTitle := textinput.New()
	altTitle.Placeholder = "Alternative anime title"
	altTitle.CharLimit = 200
	altTitle.Width = 70
	cookieInput := textinput.New()
	cookieInput.Placeholder = "cf_clearance=…; other_cookie=…"
	cookieInput.CharLimit = 16384
	cookieInput.Width = 70
	cookieInput.EchoMode = textinput.EchoPassword
	cookieInput.EchoCharacter = '•'
	userAgentInput := textinput.New()
	userAgentInput.Placeholder = "Browser User-Agent used to obtain the cookie"
	userAgentInput.CharLimit = 2048
	userAgentInput.Width = 70
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = lipgloss.NewStyle().Foreground(accent)
	newEpisodes := list.New(nil, newEpisodeDelegate(true, false, ""), 60, 8)
	newEpisodes.Title = "New Episodes"
	newEpisodes.SetShowTitle(false)
	newEpisodes.SetShowStatusBar(false)
	newEpisodes.SetFilteringEnabled(false)
	newEpisodes.SetShowHelp(false)
	newEpisodes.SetShowPagination(false)
	newEpisodes.DisableQuitKeybindings()
	newEpisodes.Styles.Title = titleStyle
	continueWatching := list.New(nil, newEpisodeDelegate(false, false, ""), 60, 8)
	continueWatching.SetShowTitle(false)
	continueWatching.SetShowStatusBar(false)
	continueWatching.SetFilteringEnabled(false)
	continueWatching.SetShowHelp(false)
	continueWatching.SetShowPagination(false)
	continueWatching.DisableQuitKeybindings()
	resumeWatching := list.New(nil, newEpisodeDelegate(false, false, ""), 60, 5)
	resumeWatching.SetShowTitle(false)
	resumeWatching.SetShowStatusBar(false)
	resumeWatching.SetFilteringEnabled(false)
	resumeWatching.SetShowHelp(false)
	resumeWatching.SetShowPagination(false)
	resumeWatching.DisableQuitKeybindings()
	episodes := list.New(nil, episodeDelegate{loading: false}, 60, 12)
	episodes.SetShowTitle(false)
	episodes.SetShowStatusBar(false)
	episodes.SetFilteringEnabled(false)
	episodes.SetShowHelp(false)
	episodes.DisableQuitKeybindings()
	return Model{
		service:              service,
		configStore:          store,
		repositoryClient:     repository.NewClient(),
		installer:            repository.NewInstaller(service),
		anilistClient:        anilist.NewClient(),
		aniskipClient:        aniskip.NewClient(),
		extensionDirectory:   extensionDirectory,
		player:               playerExecutable,
		stage:                stageSources,
		homeType:             "anime",
		homeFocus:            homeFocusNone,
		loading:              true,
		spinner:              spin,
		search:               input,
		repositoryInput:      repoInput,
		extensionSearch:      extSearch,
		tokenInput:           tokenInput,
		alternativeTitle:     altTitle,
		cookieInput:          cookieInput,
		userAgentInput:       userAgentInput,
		newEpisodesList:      newEpisodes,
		resumeWatchingList:   resumeWatching,
		continueWatchingList: continueWatching,
		episodeList:          episodes,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadRepositories())
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.search.Width = max(20, min(70, msg.Width-8))
		m.repositoryInput.Width = max(20, min(80, msg.Width-8))
		m.extensionSearch.Width = max(20, min(70, msg.Width-8))
		m.tokenInput.Width = max(20, min(80, msg.Width-8))
		m.alternativeTitle.Width = max(20, min(80, msg.Width-8))
		m.cookieInput.Width = max(20, min(80, msg.Width-8))
		m.userAgentInput.Width = max(20, min(80, msg.Width-8))
		m.resizeNewEpisodesList()
		m.resizeResumeWatchingList()
		m.resizeContinueWatchingList()
		m.resizeEpisodeList()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case sourcesMsg:
		m.loading, m.err = false, msg.err
		m.sources = msg.items
		return m, nil
	case searchMsg:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			if isCloudflareError(msg.err) {
				return m.beginCloudflareRecovery(cloudflareSearch)
			}
			return m, nil
		}
		m.results = msg.pages.List
		if m.resolvingEpisode != nil {
			if len(m.results) == 0 {
				m.stage = stageAlternativeTitle
				m.alternativeTitle.SetValue(m.resolutionQuery)
				m.alternativeTitle.Focus()
				m.err = fmt.Errorf("no match found in %s; try an alternative title", m.selectedSource.Name)
				return m, textinput.Blink
			}
			if m.manualMapping {
				m.stage, m.cursor = stageResults, 0
				m.status = "Select the correct match from " + m.selectedSource.Name
				return m, nil
			}
			m.cursor = bestMediaMatch(m.resolutionQuery, m.results)
			m.selectedMedia = m.results[m.cursor]
			m.loading = true
			m.status = ""
			return m, tea.Batch(m.spinner.Tick, m.loadDetails())
		}
		m.stage, m.cursor = stageResults, 0
		return m, nil
	case detailsMsg:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			if isCloudflareError(msg.err) {
				return m.beginCloudflareRecovery(cloudflareDetails)
			}
			return m, nil
		}
		m.details = msg.media
		m.malID = msg.malID
		if m.resolvingEpisode != nil {
			mapping := appconfig.MediaMapping{
				MediaID: m.resolvingEpisode.MediaID, SourceID: m.selectedSource.ID,
				Title: m.selectedMedia.DisplayTitle(),
			}
			if m.configStore != nil {
				if err := m.configStore.SaveMediaMapping(mapping); err != nil {
					m.err = fmt.Errorf("save media mapping: %w", err)
				}
			}
			m.mediaMappings = prependMediaMapping(m.mediaMappings, mapping)
			m.stage = stageEpisodes
			m.setEpisodeItems()
			m.cursor = episodeCursor(m.details.Episodes, nextEpisodeTargetWithLocal(*m.resolvingEpisode, m.localWatchedEpisodes()))
			m.episodeList.Select(m.cursor)
			m.status = fmt.Sprintf("Matched %s on %s", m.selectedMedia.DisplayTitle(), m.selectedSource.Name)
			return m, nil
		}
		m.stage, m.cursor = stageDetails, 0
		return m, nil
	case videosMsg:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			if isCloudflareError(msg.err) {
				return m.beginCloudflareRecovery(cloudflareVideos)
			}
			return m, nil
		}
		m.videos = msg.items
		m.stage, m.cursor = stageStreams, 0
		m.rememberStream = m.hasStreamPreference()
		if index, ok := m.automaticVideoIndex(msg.items); ok {
			m.cursor = index
			return m.playSelected()
		}
		return m, nil
	case pagesMsg:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			if isCloudflareError(msg.err) {
				return m.beginCloudflareRecovery(cloudflarePages)
			}
			return m, nil
		}
		m.pages = msg.items
		m.stage, m.cursor = stagePages, 0
		return m, nil
	case playerFinishedMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("player: %w", msg.err)
		} else {
			m.status = "Playback finished"
		}
		var syncCommand tea.Cmd
		if msg.entry != nil && msg.progress != nil && msg.progress.Duration > 0 {
			entry := *msg.entry
			entry.Position, entry.Duration = msg.progress.Position, msg.progress.Duration
			entry.UpdatedAt = time.Now().UnixMilli()
			if msg.progress.Position/msg.progress.Duration >= watchedThreshold {
				entry.WatchedEpisodes = appendUniqueString(entry.WatchedEpisodes, normalizedEpisodeNumber(entry.EpisodeNumber))
			}
			if err := m.configStore.SaveContinue(entry); err != nil {
				m.err = fmt.Errorf("save playback progress: %w", err)
			} else {
				m.continueWatching = prependContinue(m.continueWatching, entry)
				m.refreshResumeWatching()
				if len(m.details.Episodes) > 0 {
					selectedIndex := m.episodeList.Index()
					m.setEpisodeItems()
					m.episodeList.Select(min(selectedIndex, len(m.details.Episodes)-1))
					if m.stage == stageStreams || m.stage == stagePages {
						m.stage = stageEpisodes
					}
				}
			}
			syncCommand = m.syncAniListPlayback(entry, *msg.progress)
		}
		return m, syncCommand
	case repositoriesMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.repositories = msg.items
			m.anilistToken = msg.token
			m.continueWatching = msg.continueWatching
			sort.SliceStable(m.continueWatching, func(i, j int) bool {
				return m.continueWatching[i].UpdatedAt > m.continueWatching[j].UpdatedAt
			})
			m.refreshResumeWatching()
			m.activeSources = msg.activeSources
			m.mediaMappings = msg.mediaMappings
			m.sourceCredentials = msg.sourceCredentials
			m.streamPreferences = msg.streamPreferences
			commands := []tea.Cmd{m.loadSourceCredentials(msg.sourceCredentials)}
			if msg.token != "" {
				commands = append(commands, m.loadAniListLibrary(msg.token, false, true))
			}
			return m, tea.Batch(commands...)
		}
		return m, nil
	case credentialsLoadedMsg:
		if msg.err != nil {
			m.status = "Saved Cloudflare credentials could not be restored: " + msg.err.Error()
		}
		return m, m.loadSources()
	case availableExtensionsMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.availableExtensions = msg.items
			m.stage, m.cursor = stageAvailableExtensions, 0
		}
		return m, nil
	case repositorySavedMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.stage, m.cursor = stageRepositories, 0
			m.status = "Repository added"
			return m, m.loadRepositories()
		}
		return m, nil
	case repositoryRemovedMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.cursor = 0
			m.status = "Repository removed"
			return m, m.loadRepositories()
		}
		return m, nil
	case extensionInstalledMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.status = "Installed " + msg.path
			return m, m.loadSources()
		}
		return m, nil
	case extensionRemovedMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.status = "Removed " + msg.path
			return m, m.loadSources()
		}
		return m, nil
	case aniListLibraryMsg:
		if !msg.background {
			m.loading = false
		}
		m.err = msg.err
		if msg.err == nil {
			shouldPersistToken := msg.token != "" && msg.token != m.anilistToken
			m.anilistToken = msg.token
			m.viewer = msg.library.Viewer
			m.trackingEntries = msg.library.Entries
			m.refreshNewEpisodes(time.Now())
			m.refreshContinueWatching()
			if msg.open {
				m.stage, m.cursor = stageAniListLibrary, 0
			}
			if shouldPersistToken {
				if err := m.configStore.SetAniListToken(msg.token); err != nil {
					m.err = fmt.Errorf("save AniList token: %w", err)
				}
			}
		}
		return m, nil
	case aniListUpdatedMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			for i := range m.trackingEntries {
				if m.trackingEntries[i].ID == msg.entry.ID {
					m.trackingEntries[i] = msg.entry
					break
				}
			}
			m.editingEntry = msg.entry
			m.refreshNewEpisodes(time.Now())
			m.refreshContinueWatching()
			m.status = "AniList entry updated"
			m.stage = stageAniListLibrary
		}
		return m, nil
	case aniListPlaybackUpdatedMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("update AniList playback: %w", msg.err)
			return m, nil
		}
		for i := range m.trackingEntries {
			if m.trackingEntries[i].ID == msg.entry.ID {
				m.trackingEntries[i] = msg.entry
				break
			}
		}
		msg.playback.AniListProgress = msg.entry.Progress
		msg.playback.AniListStatus = msg.entry.Status
		if err := m.configStore.SaveContinue(msg.playback); err != nil {
			m.err = fmt.Errorf("save AniList playback progress: %w", err)
			return m, nil
		}
		m.continueWatching = prependContinue(m.continueWatching, msg.playback)
		if m.resolvingEpisode != nil && m.resolvingEpisode.ID == msg.entry.ID {
			updated := *m.resolvingEpisode
			updated.Progress = msg.entry.Progress
			updated.Status = msg.entry.Status
			m.resolvingEpisode = &updated
		}
		if len(m.details.Episodes) > 0 {
			selectedIndex := m.episodeList.Index()
			m.setEpisodeItems()
			m.episodeList.Select(selectedIndex)
		}
		m.refreshNewEpisodes(time.Now())
		m.refreshContinueWatching()
		m.status = fmt.Sprintf("Playback finished • AniList progress %d", msg.entry.Progress)
		return m, nil
	case aniListLogoutMsg:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.anilistToken = ""
			m.trackingEntries = nil
			m.refreshNewEpisodes(time.Now())
			m.refreshContinueWatching()
			m.viewer = anilist.Viewer{}
			m.stage, m.cursor = stageSources, 0
			m.status = "Signed out of AniList"
		}
		return m, nil
	case browserOpenedMsg:
		m.err = msg.err
		if msg.err == nil {
			if m.stage == stageCloudflareCookie || m.stage == stageCloudflareUserAgent {
				m.status = "Source domain opened in your browser"
			} else {
				m.status = "AniList authorization opened in your browser"
			}
		}
		return m, nil
	case cloudflareAppliedMsg:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.sourceCredentials = prependSourceCredential(m.sourceCredentials, msg.credential)
		m.status = "Cloudflare credentials applied; retrying request"
		m.stage = m.cloudflareReturn
		return m, m.retryCloudflareOperation()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.stage == stageSearch {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(message)
		return m, cmd
	}
	if m.stage == stageRepositoryAdd {
		var cmd tea.Cmd
		m.repositoryInput, cmd = m.repositoryInput.Update(message)
		return m, cmd
	}
	if m.stage == stageExtensionSearch {
		var cmd tea.Cmd
		m.extensionSearch, cmd = m.extensionSearch.Update(message)
		return m, cmd
	}
	if m.stage == stageAniListToken {
		var cmd tea.Cmd
		m.tokenInput, cmd = m.tokenInput.Update(message)
		return m, cmd
	}
	if m.stage == stageAlternativeTitle {
		var cmd tea.Cmd
		m.alternativeTitle, cmd = m.alternativeTitle.Update(message)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.loading {
		return m, nil
	}
	if m.stage == stageCloudflareCookie {
		switch key.String() {
		case "ctrl+u":
			m.cookieInput.SetValue("")
			m.err = nil
			return m, nil
		case "esc":
			m.cookieInput.Blur()
			m.stage = m.cloudflareReturn
			return m, nil
		case "ctrl+o":
			return m, openURL(m.selectedSource.BaseURL)
		case "enter":
			if strings.TrimSpace(m.cookieInput.Value()) == "" {
				m.err = fmt.Errorf("Cookie header is required")
				return m, nil
			}
			m.cookieInput.Blur()
			m.stage = stageCloudflareUserAgent
			m.userAgentInput.Focus()
			return m, textinput.Blink
		default:
			var cmd tea.Cmd
			m.cookieInput, cmd = m.cookieInput.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageCloudflareUserAgent {
		switch key.String() {
		case "ctrl+u":
			m.userAgentInput.SetValue("")
			m.err = nil
			return m, nil
		case "esc":
			m.userAgentInput.Blur()
			m.stage = stageCloudflareCookie
			m.cookieInput.Focus()
			return m, textinput.Blink
		case "ctrl+o":
			return m, openURL(m.selectedSource.BaseURL)
		case "enter":
			if strings.TrimSpace(m.userAgentInput.Value()) == "" {
				m.err = fmt.Errorf("the matching browser User-Agent is required")
				return m, nil
			}
			m.userAgentInput.Blur()
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.applyCloudflareCredentials())
		default:
			var cmd tea.Cmd
			m.userAgentInput, cmd = m.userAgentInput.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageSearch {
		switch key.String() {
		case "esc":
			m.stage = stageDiscoverSources
			m.search.Blur()
			return m, nil
		case "enter":
			query := strings.TrimSpace(m.search.Value())
			if query == "" {
				return m, nil
			}
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.runSearch(query))
		default:
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageAlternativeTitle {
		switch key.String() {
		case "esc":
			m.alternativeTitle.Blur()
			if len(m.details.Episodes) > 0 {
				m.stage = stageEpisodes
			} else {
				m.clearEpisodeResolution()
				m.stage = stageSources
			}
			return m, nil
		case "enter":
			query := strings.TrimSpace(m.alternativeTitle.Value())
			if query == "" {
				m.err = fmt.Errorf("alternative title is required")
				return m, nil
			}
			m.alternativeTitle.Blur()
			m.manualMapping, m.resolutionQuery = true, query
			m.loading, m.err, m.status = true, nil, ""
			return m, tea.Batch(m.spinner.Tick, m.runSearch(query))
		default:
			var cmd tea.Cmd
			m.alternativeTitle, cmd = m.alternativeTitle.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageRepositoryAdd {
		switch key.String() {
		case "esc":
			m.stage = stageRepositories
			m.repositoryInput.Blur()
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.repositoryInput.Value())
			if value == "" || (!strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://")) {
				m.err = fmt.Errorf("repository URL must start with http:// or https://")
				return m, nil
			}
			m.pendingRepositoryURL = value
			m.stage = stageRepositoryType
			m.repositoryInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.repositoryInput, cmd = m.repositoryInput.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageRepositoryType {
		switch key.String() {
		case "esc":
			m.stage = stageRepositoryAdd
			m.repositoryInput.Focus()
			return m, textinput.Blink
		case "a":
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.saveRepository("anime"))
		case "m":
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.saveRepository("manga"))
		}
		return m, nil
	}
	if m.stage == stageExtensionSearch {
		switch key.String() {
		case "esc":
			m.stage = stageAvailableExtensions
			m.extensionSearch.Blur()
			return m, nil
		case "enter":
			m.extensionQuery = strings.TrimSpace(m.extensionSearch.Value())
			m.stage, m.cursor = stageAvailableExtensions, 0
			m.extensionSearch.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.extensionSearch, cmd = m.extensionSearch.Update(key)
			m.cursor = 0
			return m, cmd
		}
	}
	if m.stage == stageAniListToken {
		switch key.String() {
		case "esc":
			m.stage = stageSources
			m.tokenInput.Blur()
			return m, nil
		case "enter":
			token := strings.TrimSpace(m.tokenInput.Value())
			if token == "" {
				m.err = fmt.Errorf("AniList token is required")
				return m, nil
			}
			m.loading, m.err = true, nil
			m.tokenInput.Blur()
			return m, tea.Batch(m.spinner.Tick, m.loadAniListLibrary(token, true, false))
		case "ctrl+o":
			return m, openAniListAuthorization()
		default:
			var cmd tea.Cmd
			m.tokenInput, cmd = m.tokenInput.Update(key)
			return m, cmd
		}
	}
	if m.stage == stageAniListEdit {
		switch key.String() {
		case "esc":
			m.stage = stageAniListLibrary
			return m, nil
		case "left", "h", "-":
			if m.editingEntry.Progress > 0 {
				m.editingEntry.Progress--
			}
			return m, nil
		case "pgdown":
			m.editingEntry.Progress = max(0, m.editingEntry.Progress-10)
			return m, nil
		case "right", "l", "+", "=":
			if m.editingEntry.Total <= 0 || m.editingEntry.Progress < m.editingEntry.Total {
				m.editingEntry.Progress++
			}
			return m, nil
		case "pgup":
			m.editingEntry.Progress += 10
			if m.editingEntry.Total > 0 {
				m.editingEntry.Progress = min(m.editingEntry.Progress, m.editingEntry.Total)
			}
			return m, nil
		case "tab", "s":
			m.editingEntry.Status = nextAniListStatus(m.editingEntry.Status)
			return m, nil
		case "enter":
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.updateAniListEntry())
		}
		return m, nil
	}

	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.back()
		return m, nil
	case "up", "k":
		if m.stage == stageSources {
			if m.homeFocus == homeFocusResumeWatching {
				if m.resumeWatchingList.Index() > 0 {
					m.resumeWatchingList.Select(m.resumeWatchingList.Index() - 1)
				}
			} else if m.homeFocus == homeFocusNewEpisodes {
				if m.newEpisodesList.Index() > 0 {
					m.newEpisodesList.Select(m.newEpisodesList.Index() - 1)
				} else if len(m.resumeWatchingList.Items()) > 0 {
					m.homeFocus = homeFocusResumeWatching
					m.resumeWatchingList.Select(len(m.resumeWatchingList.Items()) - 1)
				}
			} else if m.homeFocus == homeFocusContinueWatching {
				if m.continueWatchingList.Index() > 0 {
					m.continueWatchingList.Select(m.continueWatchingList.Index() - 1)
				} else if len(m.newEpisodesList.Items()) > 0 {
					m.homeFocus = homeFocusNewEpisodes
					m.newEpisodesList.Select(len(m.newEpisodesList.Items()) - 1)
				} else if len(m.resumeWatchingList.Items()) > 0 {
					m.homeFocus = homeFocusResumeWatching
					m.resumeWatchingList.Select(len(m.resumeWatchingList.Items()) - 1)
				}
			}
			return m, nil
		}
		if m.stage == stageEpisodes {
			m.episodeList.Select(max(0, m.episodeList.Index()-1))
			m.cursor = m.episodeList.Index()
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.stage == stageSources {
			if m.homeFocus == homeFocusResumeWatching {
				if m.resumeWatchingList.Index()+1 < len(m.resumeWatchingList.Items()) {
					m.resumeWatchingList.Select(m.resumeWatchingList.Index() + 1)
				} else if len(m.newEpisodesList.Items()) > 0 {
					m.homeFocus = homeFocusNewEpisodes
					m.newEpisodesList.Select(0)
				} else if len(m.continueWatchingList.Items()) > 0 {
					m.homeFocus = homeFocusContinueWatching
					m.continueWatchingList.Select(0)
				}
			} else if m.homeFocus == homeFocusNewEpisodes {
				if m.newEpisodesList.Index()+1 < len(m.newEpisodesList.Items()) {
					m.newEpisodesList.Select(m.newEpisodesList.Index() + 1)
				} else if len(m.continueWatchingList.Items()) > 0 {
					m.homeFocus = homeFocusContinueWatching
					m.continueWatchingList.Select(0)
				}
			} else if m.homeFocus == homeFocusContinueWatching {
				if m.continueWatchingList.Index()+1 < len(m.continueWatchingList.Items()) {
					m.continueWatchingList.Select(m.continueWatchingList.Index() + 1)
				}
			}
			return m, nil
		}
		if m.stage == stageEpisodes {
			if len(m.episodeList.Items()) > 0 {
				m.episodeList.Select(min(len(m.episodeList.Items())-1, m.episodeList.Index()+1))
			}
			m.cursor = m.episodeList.Index()
			return m, nil
		}
		if m.cursor+1 < m.currentLength() {
			m.cursor++
		}
		return m, nil
	case "tab", "left", "right", "h", "l", "1", "2":
		if m.stage == stageSources {
			if key.String() == "1" {
				m.homeType = "anime"
			} else if key.String() == "2" {
				m.homeType = "manga"
			} else if key.String() == "h" || key.String() == "left" {
				m.homeType = "anime"
			} else if key.String() == "l" || key.String() == "right" {
				m.homeType = "manga"
			} else if m.homeType == "anime" {
				m.homeType = "manga"
			} else {
				m.homeType = "anime"
			}
			m.cursor = 0
			m.resumeWatchingList.Select(0)
			m.newEpisodesList.Select(0)
			m.continueWatchingList.Select(0)
			if m.homeType == "anime" && len(m.resumeWatchingList.Items()) > 0 {
				m.homeFocus = homeFocusResumeWatching
			} else if m.homeType == "anime" && len(m.newEpisodesList.Items()) > 0 {
				m.homeFocus = homeFocusNewEpisodes
			} else if m.homeType == "anime" && len(m.continueWatchingList.Items()) > 0 {
				m.homeFocus = homeFocusContinueWatching
			} else {
				m.homeFocus = homeFocusNone
			}
			return m, nil
		}
		if key.String() == "l" && m.stage == stageAvailableExtensions {
			m.cycleExtensionLanguage()
			m.cursor = 0
			return m, nil
		}
	case "/":
		if m.stage == stageSources {
			m.stage, m.cursor = stageDiscoverSources, 0
			return m, nil
		}
		if m.stage == stageAvailableExtensions {
			m.stage = stageExtensionSearch
			m.extensionSearch.SetValue(m.extensionQuery)
			m.extensionSearch.Focus()
			return m, textinput.Blink
		}
	case "c":
		if m.stage == stageAvailableExtensions {
			m.extensionQuery, m.extensionLanguage, m.cursor = "", "", 0
			return m, nil
		}
		if m.selectedSource.ID != "" && (m.stage == stageSearch || m.stage == stageResults || m.stage == stageDetails || m.stage == stageEpisodes || m.stage == stageStreams) {
			operation := cloudflareSearch
			switch m.stage {
			case stageResults, stageDetails:
				operation = cloudflareDetails
			case stageEpisodes, stageStreams:
				operation = cloudflareVideos
			}
			return m.beginCloudflareRecovery(operation)
		}
	case "r":
		if m.stage == stageSources || m.stage == stageDiscoverSources {
			m.stage, m.cursor, m.err = stageRepositories, 0, nil
			return m, nil
		}
	case "t":
		if m.stage == stageSources {
			m.err, m.status = nil, ""
			if m.anilistToken == "" {
				m.stage = stageAniListToken
				m.tokenInput.SetValue("")
				m.tokenInput.Focus()
				return m, textinput.Blink
			}
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadAniListLibrary(m.anilistToken, true, false))
		}
	case "w":
		if m.stage == stageSources && m.homeType == "anime" && len(m.continueWatching) > 0 {
			return m.playContinue(m.continueWatching[0])
		}
	case "x":
		if m.stage == stageAniListLibrary {
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.logoutAniList())
		}
	case "a":
		if (m.stage == stageEpisodes || m.stage == stageDetails || m.stage == stageResults) && m.resolvingEpisode != nil {
			m.stage, m.err, m.status = stageAlternativeTitle, nil, ""
			m.alternativeTitle.SetValue(m.resolutionQuery)
			m.alternativeTitle.Focus()
			return m, textinput.Blink
		}
		if m.stage == stageRepositories {
			m.stage, m.err = stageRepositoryAdd, nil
			m.repositoryInput.SetValue("")
			m.repositoryInput.Focus()
			return m, textinput.Blink
		}
	case "d":
		if m.stage == stageSources {
			m.stage, m.cursor, m.err, m.status = stageDiscoverSources, 0, nil, ""
			return m, nil
		}
		if m.stage == stageRepositories && len(m.repositories) > 0 {
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.removeRepository(m.repositories[m.cursor]))
		}
		if m.stage == stageAvailableExtensions {
			items := m.filteredExtensions()
			if len(items) == 0 || !m.installer.IsInstalled(items[m.cursor], m.extensionDirectory) {
				m.status = "Selected extension is not installed"
				return m, nil
			}
			m.loading, m.err = true, nil
			return m, tea.Batch(m.spinner.Tick, m.uninstallExtension(items[m.cursor]))
		}
	case "enter":
		return m.activate()
	case "s":
		if (m.stage == stageEpisodes || m.stage == stageDetails || m.stage == stageResults) && m.resolvingEpisode != nil {
			m.stage, m.cursor, m.err, m.status = stageEpisodeSource, 0, nil, ""
			for index, source := range m.animeSources() {
				if source.ID == m.selectedSource.ID {
					m.cursor = index
					break
				}
			}
			return m, nil
		}
	case " ", "space":
		if m.stage == stageStreams {
			m.rememberStream = !m.rememberStream
			if !m.rememberStream {
				m.clearStreamPreference()
			}
			return m, nil
		}
	case "p":
		if m.stage == stageStreams {
			return m.activate()
		}
	}
	return m, nil
}

func openAniListAuthorization() tea.Cmd {
	return func() tea.Msg {
		return browserOpenedMsg{err: browser.Open(anilist.AuthorizeURL)}
	}
}

func (m Model) activate() (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageSources:
		if m.homeFocus == homeFocusResumeWatching && m.homeType == "anime" {
			selected, ok := m.resumeWatchingList.SelectedItem().(resumeWatchingListItem)
			if ok {
				return m.playContinue(selected.entry)
			}
		}
		if m.homeFocus == homeFocusNewEpisodes && m.homeType == "anime" {
			return m.activateNewEpisode()
		}
		if m.homeFocus == homeFocusContinueWatching && m.homeType == "anime" {
			selected, ok := m.continueWatchingList.SelectedItem().(continueWatchingListItem)
			if ok {
				return m.activateTrackedEpisode(selected.entry)
			}
		}
		return m, nil
	case stageDiscoverSources:
		sources := m.homeSources()
		if len(sources) == 0 {
			return m, nil
		}
		m.clearEpisodeResolution()
		m.selectedSource = sources[m.cursor]
		m.rememberActiveSource(m.selectedSource)
		m.stage = stageSearch
		m.search.SetValue("")
		m.search.Focus()
		return m, textinput.Blink
	case stageResults:
		if len(m.results) == 0 {
			return m, nil
		}
		m.selectedMedia = m.results[m.cursor]
		m.loading, m.err = true, nil
		return m, tea.Batch(m.spinner.Tick, m.loadDetails())
	case stageEpisodeSource:
		sources := m.animeSources()
		if len(sources) == 0 || m.resolvingEpisode == nil {
			return m, nil
		}
		m.selectedSource = sources[m.cursor]
		m.rememberActiveSource(m.selectedSource)
		m.manualMapping = false
		m.resolutionQuery = m.mappingTitle(m.resolvingEpisode.MediaID, m.selectedSource.ID, m.resolvingEpisode.Title)
		m.loading, m.err = true, nil
		return m, tea.Batch(m.spinner.Tick, m.runSearch(m.resolutionQuery))
	case stageDetails:
		if len(m.details.Episodes) > 0 {
			m.stage, m.cursor = stageEpisodes, 0
			m.setEpisodeItems()
			m.episodeList.Select(0)
		}
		return m, nil
	case stageEpisodes:
		selected, ok := m.episodeList.SelectedItem().(episodeListItem)
		if !ok {
			return m, nil
		}
		m.selectedEpisode = selected.episode
		m.loading, m.err = true, nil
		if m.selectedSource.IsAnime() {
			return m, tea.Batch(m.spinner.Tick, m.loadVideos())
		}
		return m, tea.Batch(m.spinner.Tick, m.loadPages())
	case stageStreams:
		if len(m.videos) == 0 {
			return m, nil
		}
		m.rememberSelectedStream()
		return m.playSelected()
	case stageRepositories:
		if len(m.repositories) == 0 {
			return m, nil
		}
		m.selectedRepository = m.repositories[m.cursor]
		m.loading, m.err = true, nil
		return m, tea.Batch(m.spinner.Tick, m.fetchRepository())
	case stageAvailableExtensions:
		items := m.filteredExtensions()
		if len(items) == 0 {
			return m, nil
		}
		m.loading, m.err = true, nil
		return m, tea.Batch(m.spinner.Tick, m.installExtension(items[m.cursor]))
	case stageAniListLibrary:
		if len(m.trackingEntries) == 0 {
			return m, nil
		}
		m.editingEntry = m.trackingEntries[m.cursor]
		m.stage = stageAniListEdit
		return m, nil
	}
	return m, nil
}

func (m Model) activateNewEpisode() (tea.Model, tea.Cmd) {
	selected, ok := m.newEpisodesList.SelectedItem().(newEpisodeListItem)
	if !ok {
		return m, nil
	}
	return m.activateTrackedEpisode(selected.entry)
}

func (m Model) activateTrackedEpisode(entry anilist.Entry) (tea.Model, tea.Cmd) {
	sources := m.animeSources()
	if len(sources) == 0 {
		m.err = fmt.Errorf("install an anime source to open this title")
		return m, nil
	}
	m.resolvingEpisode = &entry
	m.manualMapping = false
	m.details = catalog.Media{}
	m.selectedSource = m.sourceForMedia(entry.MediaID, sources)
	m.rememberActiveSource(m.selectedSource)
	m.resolutionQuery = m.mappingTitle(entry.MediaID, m.selectedSource.ID, entry.Title)
	m.search.SetValue(m.resolutionQuery)
	m.loading, m.err = true, nil
	return m, tea.Batch(m.spinner.Tick, m.runSearch(m.resolutionQuery))
}

func (m Model) playSelected() (tea.Model, tea.Cmd) {
	if len(m.videos) == 0 {
		return m, nil
	}
	video := m.videos[m.cursor]
	entry := appconfig.ContinueEntry{
		Title:         firstNonEmpty(m.details.DisplayTitle(), m.selectedMedia.DisplayTitle()),
		SourceID:      m.selectedSource.ID,
		SourceName:    m.selectedSource.Name,
		MediaURL:      firstNonEmpty(m.details.URL, m.selectedMedia.URL),
		EpisodeURL:    m.selectedEpisode.URL,
		EpisodeName:   m.selectedEpisode.Name,
		EpisodeNumber: m.selectedEpisode.Number(),
		VideoURL:      video.URL,
		Quality:       video.Quality,
		Headers:       video.Headers,
		UpdatedAt:     time.Now().UnixMilli(),
	}
	entry.WatchedEpisodes = m.watchedEpisodesFor(entry.SourceID, entry.MediaURL)
	if m.resolvingEpisode != nil {
		entry.AniListEntryID = m.resolvingEpisode.ID
		entry.AniListMediaID = m.resolvingEpisode.MediaID
		entry.AniListProgress = m.resolvingEpisode.Progress
		entry.AniListTotal = m.resolvingEpisode.Total
		entry.AniListStatus = m.resolvingEpisode.Status
		entry.AniListMediaStatus = m.resolvingEpisode.MediaStatus
	}
	for _, subtitle := range video.Subtitles {
		if subtitle.File != "" {
			entry.Subtitles = append(entry.Subtitles, subtitle.File)
		}
	}
	if err := m.configStore.SaveContinue(entry); err != nil {
		m.err = fmt.Errorf("save Continue Watching: %w", err)
		return m, nil
	}
	m.continueWatching = prependContinue(m.continueWatching, entry)
	m.refreshResumeWatching()
	return m.startPlayer(video, entry)
}

func (m Model) playContinue(entry appconfig.ContinueEntry) (tea.Model, tea.Cmd) {
	video := catalog.Video{URL: entry.VideoURL, Quality: entry.Quality, Headers: entry.Headers}
	for _, subtitle := range entry.Subtitles {
		video.Subtitles = append(video.Subtitles, catalog.Track{File: subtitle})
	}
	entry.UpdatedAt = time.Now().UnixMilli()
	if err := m.configStore.SaveContinue(entry); err != nil {
		m.err = fmt.Errorf("save Continue Watching: %w", err)
		return m, nil
	}
	m.continueWatching = prependContinue(m.continueWatching, entry)
	m.refreshResumeWatching()
	return m.startPlayer(video, entry)
}

func (m Model) startPlayer(video catalog.Video, entry appconfig.ContinueEntry) (tea.Model, tea.Cmd) {
	session, err := player.MPVSession(m.player, video, m.skipChapters(entry.Duration)...)
	if err != nil {
		m.err = err
		return m, nil
	}
	return m, tea.ExecProcess(session.Command, func(runErr error) tea.Msg {
		progress, progressErr := session.Progress()
		if progressErr != nil {
			return playerFinishedMsg{err: errors.Join(runErr, progressErr), entry: &entry}
		}
		return playerFinishedMsg{err: runErr, progress: &progress, entry: &entry}
	})
}

// skipChapters mirrors AnymeX's AniSkip lookup and turns the provider-agnostic
// skip ranges into mpv chapter markers.
func (m Model) skipChapters(episodeLength float64) []player.Chapter {
	if m.aniskipClient == nil || m.resolvingEpisode == nil || m.malID == 0 {
		return nil
	}
	number, err := strconv.ParseFloat(normalizedEpisodeNumber(m.selectedEpisode.Number()), 64)
	if err != nil || number <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	times, err := m.aniskipClient.Times(ctx, m.malID, number, int(episodeLength))
	if err != nil || times.Empty() {
		return nil
	}
	return skipTimesChapters(times)
}

func skipTimesChapters(times aniskip.Times) []player.Chapter {
	type segment struct {
		name     string
		interval *aniskip.Interval
	}
	segments := make([]segment, 0, 3)
	if times.Recap != nil {
		segments = append(segments, segment{"Recap", times.Recap})
	}
	if times.Op != nil {
		segments = append(segments, segment{"Intro", times.Op})
	}
	if times.Ed != nil {
		segments = append(segments, segment{"Outro", times.Ed})
	}
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].interval.Start < segments[j].interval.Start
	})
	chapters := make([]player.Chapter, 0, len(segments)*2)
	for _, item := range segments {
		chapters = append(chapters,
			player.Chapter{Start: item.interval.Start, End: item.interval.End, Title: item.name},
			player.Chapter{Start: item.interval.End, End: item.interval.End, Title: item.name + " end"},
		)
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		return chapters[i].Start < chapters[j].Start
	})
	return chapters
}

func prependContinue(entries []appconfig.ContinueEntry, entry appconfig.ContinueEntry) []appconfig.ContinueEntry {
	result := []appconfig.ContinueEntry{entry}
	for _, existing := range entries {
		if existing.SourceID == entry.SourceID && existing.MediaURL == entry.MediaURL {
			continue
		}
		result = append(result, existing)
		if len(result) == 10 {
			break
		}
	}
	return result
}

func prependStreamPreference(items []appconfig.StreamPreference, preference appconfig.StreamPreference) []appconfig.StreamPreference {
	result := []appconfig.StreamPreference{preference}
	for _, existing := range items {
		if existing.MediaID != preference.MediaID {
			result = append(result, existing)
		}
	}
	return result
}

func removeStreamPreference(items []appconfig.StreamPreference, mediaID int) []appconfig.StreamPreference {
	filtered := make([]appconfig.StreamPreference, 0, len(items))
	for _, existing := range items {
		if existing.MediaID != mediaID {
			filtered = append(filtered, existing)
		}
	}
	return filtered
}

func (m Model) currentMediaID() int {
	if m.resolvingEpisode == nil {
		return 0
	}
	return m.resolvingEpisode.MediaID
}

func (m Model) savedStreamValue() (string, bool) {
	mediaID := m.currentMediaID()
	if mediaID == 0 {
		return "", false
	}
	for _, preference := range m.streamPreferences {
		if preference.MediaID == mediaID && preference.Value != "" {
			return preference.Value, true
		}
	}
	return "", false
}

func (m Model) hasStreamPreference() bool {
	_, ok := m.savedStreamValue()
	return ok
}

func (m *Model) rememberSelectedStream() {
	if !m.rememberStream || len(m.videos) == 0 {
		return
	}
	mediaID := m.currentMediaID()
	value := streamLabel(m.videos[m.cursor])
	if mediaID == 0 || value == "" {
		return
	}
	preference := appconfig.StreamPreference{MediaID: mediaID, Value: value}
	if m.configStore != nil {
		if err := m.configStore.SaveStreamPreference(preference); err != nil {
			m.err = fmt.Errorf("remember stream preference: %w", err)
			return
		}
	}
	m.streamPreferences = prependStreamPreference(m.streamPreferences, preference)
}

func (m *Model) clearStreamPreference() {
	mediaID := m.currentMediaID()
	if mediaID == 0 {
		return
	}
	if m.configStore != nil {
		if err := m.configStore.ClearStreamPreference(mediaID); err != nil {
			m.err = fmt.Errorf("clear stream preference: %w", err)
		}
	}
	m.streamPreferences = removeStreamPreference(m.streamPreferences, mediaID)
}

func (m Model) automaticVideoIndex(videos []catalog.Video) (int, bool) {
	if saved, ok := m.savedStreamValue(); ok {
		wantDub := isDubLabel(saved)
		for index, video := range videos {
			if wantDub && !isDubLabel(streamLabel(video)) {
				continue
			}
			if streamMatches(video, saved) {
				return index, true
			}
		}
	}
	if len(videos) == 1 {
		return 0, true
	}
	return 0, false
}

func streamLabel(video catalog.Video) string {
	return strings.TrimSpace(firstNonEmpty(video.Quality, video.URL))
}

func streamMatches(video catalog.Video, saved string) bool {
	if saved == "" {
		return false
	}
	return strings.EqualFold(streamLabel(video), saved) || (video.URL != "" && video.URL == saved)
}

func isDubLabel(label string) bool {
	s := strings.ToLower(label)
	return strings.Contains(s, "dub") ||
		(strings.Contains(s, "english") && !strings.Contains(s, "sub")) ||
		(strings.Contains(s, "eng") && !strings.Contains(s, "sub"))
}

func (m *Model) back() {
	m.err = nil
	m.status = ""
	m.cursor = 0
	switch m.stage {
	case stageResults:
		if m.resolvingEpisode != nil {
			if m.manualMapping {
				m.stage = stageAlternativeTitle
				m.alternativeTitle.SetValue(m.resolutionQuery)
				m.alternativeTitle.Focus()
			} else {
				m.clearEpisodeResolution()
				m.stage = stageSources
			}
		} else {
			m.stage = stageSearch
			m.search.Focus()
		}
	case stageDetails:
		if m.resolvingEpisode != nil {
			m.clearEpisodeResolution()
			m.stage = stageSources
		} else {
			m.stage = stageResults
		}
	case stageEpisodes:
		if m.resolvingEpisode != nil {
			m.clearEpisodeResolution()
			m.stage = stageSources
		} else {
			m.stage = stageDetails
		}
	case stageStreams, stagePages:
		m.stage = stageEpisodes
	case stageRepositories:
		m.stage = stageSources
	case stageDiscoverSources:
		m.stage = stageSources
	case stageRepositoryAdd:
		m.stage = stageRepositories
	case stageRepositoryType:
		m.stage = stageRepositoryAdd
	case stageAvailableExtensions:
		m.stage = stageRepositories
	case stageExtensionSearch:
		m.stage = stageAvailableExtensions
	case stageAniListToken, stageAniListLibrary:
		m.stage = stageSources
	case stageAniListEdit:
		m.stage = stageAniListLibrary
	case stageEpisodeSource, stageAlternativeTitle:
		m.alternativeTitle.Blur()
		if len(m.details.Episodes) > 0 {
			m.stage = stageEpisodes
		} else {
			m.clearEpisodeResolution()
			m.stage = stageSources
		}
	}
}

func (m *Model) clearEpisodeResolution() {
	m.resolvingEpisode = nil
	m.manualMapping = false
	m.resolutionQuery = ""
	m.status = ""
}

func (m Model) currentLength() int {
	switch m.stage {
	case stageSources:
		return len(m.resumeWatchingList.Items()) + len(m.newEpisodesList.Items()) + len(m.continueWatchingList.Items())
	case stageDiscoverSources:
		return len(m.homeSources())
	case stageResults:
		return len(m.results)
	case stageEpisodes:
		return len(m.episodeList.Items())
	case stageStreams:
		return len(m.videos)
	case stagePages:
		return len(m.pages)
	case stageRepositories:
		return len(m.repositories)
	case stageAvailableExtensions:
		return len(m.filteredExtensions())
	case stageAniListLibrary:
		return len(m.trackingEntries)
	case stageEpisodeSource:
		return len(m.animeSources())
	default:
		return 0
	}
}

func (m Model) homeSources() []catalog.Source {
	result := make([]catalog.Source, 0, len(m.sources))
	for _, source := range m.sources {
		if strings.EqualFold(source.Type, m.homeType) {
			result = append(result, source)
		}
	}
	return result
}

func (m Model) animeSources() []catalog.Source {
	result := make([]catalog.Source, 0, len(m.sources))
	for _, source := range m.sources {
		if source.IsAnime() {
			result = append(result, source)
		}
	}
	return result
}

func (m Model) sourceForMedia(mediaID int, sources []catalog.Source) catalog.Source {
	for _, mapping := range m.mediaMappings {
		if mapping.MediaID != mediaID {
			continue
		}
		for _, source := range sources {
			if source.ID == mapping.SourceID {
				return source
			}
		}
	}
	if sourceID := m.activeSources["anime"]; sourceID != "" {
		for _, source := range sources {
			if source.ID == sourceID {
				return source
			}
		}
	}
	if m.selectedSource.IsAnime() {
		for _, source := range sources {
			if source.ID == m.selectedSource.ID {
				return source
			}
		}
	}
	return sources[0]
}

func (m Model) mappingTitle(mediaID int, sourceID, fallback string) string {
	for _, mapping := range m.mediaMappings {
		if mapping.MediaID == mediaID && mapping.SourceID == sourceID && mapping.Title != "" {
			return mapping.Title
		}
	}
	return fallback
}

func (m *Model) rememberActiveSource(source catalog.Source) {
	if m.activeSources == nil {
		m.activeSources = make(map[string]string)
	}
	itemType := strings.ToLower(source.Type)
	m.activeSources[itemType] = source.ID
	if m.configStore != nil {
		if err := m.configStore.SetActiveSource(itemType, source.ID); err != nil {
			m.err = fmt.Errorf("save active source: %w", err)
		}
	}
}

func prependMediaMapping(mappings []appconfig.MediaMapping, mapping appconfig.MediaMapping) []appconfig.MediaMapping {
	result := []appconfig.MediaMapping{mapping}
	for _, existing := range mappings {
		if existing.MediaID == mapping.MediaID && existing.SourceID == mapping.SourceID {
			continue
		}
		result = append(result, existing)
	}
	return result
}

func bestMediaMatch(query string, results []catalog.Media) int {
	query = normalizeTitle(query)
	bestIndex, bestScore := 0, -1
	for index, media := range results {
		score := titleMatchScore(query, normalizeTitle(media.DisplayTitle()))
		if score > bestScore {
			bestIndex, bestScore = index, score
		}
	}
	return bestIndex
}

func normalizeTitle(value string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == ' ' {
			normalized.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(normalized.String()), " ")
}

func titleMatchScore(query, title string) int {
	if query == title {
		return 10000
	}
	score := 0
	if strings.Contains(title, query) || strings.Contains(query, title) {
		score += 1000
	}
	queryWords := make(map[string]bool)
	for _, word := range strings.Fields(query) {
		queryWords[word] = true
	}
	for _, word := range strings.Fields(title) {
		if queryWords[word] {
			score += len(word) * 10
		}
	}
	difference := len(title) - len(query)
	if difference < 0 {
		difference = -difference
	}
	return score - difference
}

func episodeCursor(episodes []catalog.Episode, target int) int {
	if target <= 0 {
		return 0
	}
	for index, episode := range episodes {
		if strings.TrimSuffix(episode.Number(), ".0") == fmt.Sprint(target) {
			return index
		}
	}
	return 0
}

func nextEpisodeTarget(entry anilist.Entry) int {
	if entry.Available > entry.Progress || entry.Total > entry.Progress {
		return entry.Progress + 1
	}
	return entry.Progress
}

func nextEpisodeTargetWithLocal(entry anilist.Entry, locallyWatched []string) int {
	progress := entry.Progress
	for _, episode := range locallyWatched {
		value, err := strconv.ParseFloat(normalizedEpisodeNumber(episode), 64)
		if err == nil {
			progress = max(progress, int(value))
		}
	}
	entry.Progress = progress
	return nextEpisodeTarget(entry)
}

func normalizedEpisodeNumber(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, ".0"))
	return value
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if normalizedEpisodeNumber(existing) == normalizedEpisodeNumber(value) {
			return values
		}
	}
	return append(values, value)
}

func (m Model) watchedEpisodesFor(sourceID, mediaURL string) []string {
	for _, entry := range m.continueWatching {
		if entry.SourceID == sourceID && entry.MediaURL == mediaURL {
			watched := append([]string(nil), entry.WatchedEpisodes...)
			if entry.Duration > 0 && entry.Position/entry.Duration >= watchedThreshold {
				watched = appendUniqueString(watched, normalizedEpisodeNumber(entry.EpisodeNumber))
			}
			return watched
		}
	}
	return nil
}

func (m Model) localWatchedEpisodes() []string {
	mediaURL := firstNonEmpty(m.details.URL, m.selectedMedia.URL)
	return m.watchedEpisodesFor(m.selectedSource.ID, mediaURL)
}

type newEpisodeEntry struct {
	entry       anilist.Entry
	releaseDate *time.Time
}

func (m *Model) refreshNewEpisodes(now time.Time) {
	entries := m.newEpisodeEntries(now)
	items := make([]list.Item, len(entries))
	for index, entry := range entries {
		items[index] = newEpisodeListItem(entry)
	}
	_ = m.newEpisodesList.SetItems(items)
	m.resizeNewEpisodesList()
	m.normalizeHomeFocus()
}

func (m *Model) refreshContinueWatching() {
	entries := make([]anilist.Entry, 0)
	for _, entry := range m.trackingEntries {
		if strings.EqualFold(entry.Type, "ANIME") && strings.EqualFold(entry.Status, "CURRENT") {
			entries = append(entries, entry)
		}
	}
	items := make([]list.Item, len(entries))
	for index, entry := range entries {
		items[index] = continueWatchingListItem{entry: entry}
	}
	_ = m.continueWatchingList.SetItems(items)
	m.resizeContinueWatchingList()
	m.normalizeHomeFocus()
}

func (m *Model) refreshResumeWatching() {
	items := make([]list.Item, 0, len(m.continueWatching))
	for _, entry := range m.continueWatching {
		if entry.Duration > 0 && entry.Position/entry.Duration >= 0.99 {
			continue
		}
		items = append(items, resumeWatchingListItem{entry: entry})
	}
	_ = m.resumeWatchingList.SetItems(items)
	m.resizeResumeWatchingList()
	m.normalizeHomeFocus()
}

func (m *Model) normalizeHomeFocus() {
	if m.homeType != "anime" {
		m.homeFocus = homeFocusNone
		return
	}
	if m.homeFocus == homeFocusResumeWatching && len(m.resumeWatchingList.Items()) > 0 {
		return
	}
	if m.homeFocus == homeFocusNewEpisodes && len(m.newEpisodesList.Items()) > 0 {
		return
	}
	if m.homeFocus == homeFocusContinueWatching && len(m.continueWatchingList.Items()) > 0 {
		return
	}
	if len(m.resumeWatchingList.Items()) > 0 {
		m.homeFocus = homeFocusResumeWatching
	} else if len(m.newEpisodesList.Items()) > 0 {
		m.homeFocus = homeFocusNewEpisodes
	} else if len(m.continueWatchingList.Items()) > 0 {
		m.homeFocus = homeFocusContinueWatching
	} else {
		m.homeFocus = homeFocusNone
	}
}

func (m *Model) resizeNewEpisodesList() {
	itemCount := min(4, len(m.newEpisodesList.Items()))
	height := itemCount * 3
	m.newEpisodesList.SetSize(max(32, m.width-8), max(3, height))
}

func (m *Model) resizeContinueWatchingList() {
	itemCount := min(4, len(m.continueWatchingList.Items()))
	m.continueWatchingList.SetSize(max(32, m.width-8), max(3, itemCount*3))
}

func (m *Model) resizeResumeWatchingList() {
	itemCount := min(3, len(m.resumeWatchingList.Items()))
	m.resumeWatchingList.SetSize(max(32, m.width-8), max(3, itemCount*3))
}

func newEpisodeDelegate(focused, loading bool, spinnerView string) list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(1)
	if focused {
		delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(accent).BorderLeftForeground(accent)
		delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#C9C2F8")).BorderLeftForeground(accent)
		if loading {
			border := delegate.Styles.SelectedTitle.GetBorderStyle()
			border.Left = spinnerView
			delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.BorderStyle(border)
		}
	} else {
		delegate.Styles.SelectedTitle = delegate.Styles.NormalTitle.PaddingLeft(2)
		delegate.Styles.SelectedDesc = delegate.Styles.NormalDesc.PaddingLeft(2)
	}
	return delegate
}

func (m Model) newEpisodeEntries(now time.Time) []newEpisodeEntry {
	result := make([]newEpisodeEntry, 0)
	for _, entry := range m.trackingEntries {
		if strings.EqualFold(entry.Type, "MANGA") || entry.MediaID == 0 || !isWatching(entry.Status) {
			continue
		}
		latestReleased := entry.Available
		if latestReleased <= entry.Progress {
			continue
		}
		releaseDate := calculateReleaseDate(entry, latestReleased, now)
		mediaStatus := strings.ToUpper(entry.MediaStatus)
		isStillAiring := mediaStatus == "RELEASING" || mediaStatus == "AIRING" || entry.NextAiringEpisode != nil
		if !isStillAiring && (releaseDate == nil || now.After(releaseDate.Add(7*24*time.Hour))) {
			continue
		}
		result = append(result, newEpisodeEntry{entry: entry, releaseDate: releaseDate})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i].releaseDate, result[j].releaseDate
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		return left.After(*right)
	})
	return result
}

func isWatching(status string) bool {
	switch strings.ToUpper(status) {
	case "CURRENT", "WATCHING", "REPEATING", "REWATCHING":
		return true
	default:
		return false
	}
}

func calculateReleaseDate(entry anilist.Entry, latestReleased int, now time.Time) *time.Time {
	if next := entry.NextAiringEpisode; next != nil && next.AiringAt > 0 {
		nextDate := time.Unix(next.AiringAt, 0)
		if nextDate.Before(now) {
			return &nextDate
		}
		episodeDifference := min(52, max(0, next.Episode-latestReleased))
		released := nextDate.Add(-time.Duration(episodeDifference) * 7 * 24 * time.Hour)
		if released.After(now) {
			released = released.Add(-7 * 24 * time.Hour)
		}
		return &released
	}
	if entry.EndDate != nil && entry.EndDate.Before(now) {
		return entry.EndDate
	}
	status := strings.ToUpper(entry.MediaStatus)
	if status != "COMPLETED" && status != "FINISHED" && entry.StartDate != nil && entry.StartDate.Before(now) {
		return entry.StartDate
	}
	return nil
}

func (m Model) filteredExtensions() []repository.Extension {
	queryValue := m.extensionQuery
	if m.stage == stageExtensionSearch {
		queryValue = m.extensionSearch.Value()
	}
	query := strings.ToLower(strings.TrimSpace(queryValue))
	result := make([]repository.Extension, 0, len(m.availableExtensions))
	for _, extension := range m.availableExtensions {
		if m.extensionLanguage != "" && !strings.EqualFold(extension.Language, m.extensionLanguage) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(extension.Name+" "+extension.Package), query) {
			continue
		}
		result = append(result, extension)
	}
	return result
}

func (m *Model) cycleExtensionLanguage() {
	seen := map[string]bool{}
	languages := []string{""}
	for _, extension := range m.availableExtensions {
		language := strings.ToUpper(strings.TrimSpace(extension.Language))
		if language != "" && !seen[language] {
			seen[language] = true
			languages = append(languages, language)
		}
	}
	sort.Strings(languages[1:])
	index := 0
	for i, language := range languages {
		if strings.EqualFold(language, m.extensionLanguage) {
			index = i
			break
		}
	}
	m.extensionLanguage = languages[(index+1)%len(languages)]
}

func (m Model) loadSources() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		items, err := m.service.Sources(ctx, m.extensionDirectory)
		return sourcesMsg{items: items, err: err}
	}
}

func (m Model) loadRepositories() tea.Cmd {
	return func() tea.Msg {
		config, err := m.configStore.Load()
		return repositoriesMsg{
			items: config.Repositories, token: config.AniListToken,
			continueWatching: config.ContinueWatching, activeSources: config.ActiveSources,
			mediaMappings: config.MediaMappings, sourceCredentials: config.SourceCredentials,
			streamPreferences: config.StreamPreferences, err: err,
		}
	}
}

func (m Model) loadSourceCredentials(credentials []appconfig.SourceCredential) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, credential := range credentials {
			if err := m.service.SetCookies(ctx, credential.URL, credential.Cookie); err != nil {
				return credentialsLoadedMsg{err: fmt.Errorf("%s cookies: %w", credential.SourceID, err)}
			}
			if err := m.service.SetUserAgent(ctx, credential.URL, credential.UserAgent); err != nil {
				return credentialsLoadedMsg{err: fmt.Errorf("%s User-Agent: %w", credential.SourceID, err)}
			}
		}
		return credentialsLoadedMsg{}
	}
}

func (m Model) saveRepository(itemType string) tea.Cmd {
	return func() tea.Msg {
		repo := appconfig.Repository{URL: m.pendingRepositoryURL, Type: itemType}
		items, err := m.repositoryClient.Fetch(repo.URL, repo.Type)
		if err == nil && len(items) == 0 {
			err = fmt.Errorf("repository contains no %s extensions", itemType)
		}
		if err == nil {
			err = m.configStore.Add(repo)
		}
		return repositorySavedMsg{err: err}
	}
}

func (m Model) removeRepository(repo appconfig.Repository) tea.Cmd {
	return func() tea.Msg {
		return repositoryRemovedMsg{err: m.configStore.Remove(repo)}
	}
}

func (m Model) fetchRepository() tea.Cmd {
	return func() tea.Msg {
		items, err := m.repositoryClient.Fetch(m.selectedRepository.URL, m.selectedRepository.Type)
		return availableExtensionsMsg{items: items, err: err}
	}
}

func (m Model) installExtension(extension repository.Extension) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		path, err := m.installer.Install(ctx, extension, m.extensionDirectory)
		return extensionInstalledMsg{path: path, err: err}
	}
}

func (m Model) uninstallExtension(extension repository.Extension) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		for _, source := range m.sources {
			if source.Package != extension.Package && !containsString(extension.SourceIDs, source.ID) {
				continue
			}
			if err := m.service.UnloadExtension(ctx, source.ID); err != nil {
				return extensionRemovedMsg{err: fmt.Errorf("unload %s: %w", source.Name, err)}
			}
		}
		path, err := m.installer.Uninstall(extension, m.extensionDirectory)
		return extensionRemovedMsg{path: path, err: err}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (m Model) loadAniListLibrary(token string, open, background bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		library, err := m.anilistClient.Library(ctx, token)
		return aniListLibraryMsg{library: library, token: token, open: open, background: background, err: err}
	}
}

func (m Model) updateAniListEntry() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		entry, err := m.anilistClient.Update(ctx, m.anilistToken, m.editingEntry)
		return aniListUpdatedMsg{entry: entry, err: err}
	}
}

func (m Model) syncAniListPlayback(playback appconfig.ContinueEntry, progress player.Progress) tea.Cmd {
	if m.anilistToken == "" || playback.AniListEntryID == 0 {
		return nil
	}
	currentProgress := playback.AniListProgress
	var existingEntry *anilist.Entry
	for _, entry := range m.trackingEntries {
		if entry.ID == playback.AniListEntryID {
			copyOfEntry := entry
			existingEntry = &copyOfEntry
			currentProgress = max(currentProgress, entry.Progress)
			playback.AniListStatus = entry.Status
			playback.AniListTotal = entry.Total
			playback.AniListMediaStatus = entry.MediaStatus
			break
		}
	}
	target, ok := aniListPlaybackTarget(playback, progress, currentProgress)
	if !ok {
		return nil
	}
	if existingEntry != nil {
		progressValue, status := target.Progress, target.Status
		target = *existingEntry
		target.Progress, target.Status = progressValue, status
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		updated, err := m.anilistClient.Update(ctx, m.anilistToken, target)
		return aniListPlaybackUpdatedMsg{entry: updated, playback: playback, err: err}
	}
}

const watchedThreshold = 0.90

func aniListPlaybackTarget(playback appconfig.ContinueEntry, progress player.Progress, currentProgress int) (anilist.Entry, bool) {
	if progress.Duration <= 0 || progress.Position/progress.Duration < watchedThreshold {
		return anilist.Entry{}, false
	}
	episodeValue, err := strconv.ParseFloat(strings.TrimSpace(playback.EpisodeNumber), 64)
	if err != nil || episodeValue <= 0 {
		return anilist.Entry{}, false
	}
	episode := int(episodeValue)
	status := strings.ToUpper(playback.AniListStatus)
	mediaStatus := strings.ToUpper(playback.AniListMediaStatus)
	isFinal := playback.AniListTotal > 0 && episode >= playback.AniListTotal &&
		(mediaStatus == "COMPLETED" || mediaStatus == "FINISHED")
	if episode <= currentProgress && !(isFinal && status != "COMPLETED" && status != "REPEATING") {
		return anilist.Entry{}, false
	}
	if status != "REPEATING" {
		if isFinal {
			status = "COMPLETED"
		} else {
			status = "CURRENT"
		}
	}
	return anilist.Entry{
		ID:          playback.AniListEntryID,
		MediaID:     playback.AniListMediaID,
		Progress:    max(currentProgress, episode),
		Total:       playback.AniListTotal,
		Status:      status,
		MediaStatus: playback.AniListMediaStatus,
	}, true
}

func (m Model) logoutAniList() tea.Cmd {
	return func() tea.Msg {
		return aniListLogoutMsg{err: m.configStore.SetAniListToken("")}
	}
}

var aniListStatuses = []string{"CURRENT", "COMPLETED", "PLANNING", "PAUSED", "DROPPED", "REPEATING"}

func nextAniListStatus(current string) string {
	for index, status := range aniListStatuses {
		if status == current {
			return aniListStatuses[(index+1)%len(aniListStatuses)]
		}
	}
	return aniListStatuses[0]
}

func (m Model) runSearch(query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		pages, err := m.service.Search(ctx, m.selectedSource, query, 1)
		return searchMsg{pages: pages, err: err}
	}
}

func isCloudflareError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"cloudflare", "cf_clearance", "challenge", "captcha", "http 403", "status 403", "http 429", "status 429", "too many requests", "http 503", "status 503"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (m Model) beginCloudflareRecovery(operation cloudflareOperation) (tea.Model, tea.Cmd) {
	if errors.Is(m.err, context.DeadlineExceeded) {
		m.status = "Cloudflare bypass failed or the source did not respond in time."
	}
	m.cloudflareOperation, m.cloudflareReturn = operation, m.stage
	m.stage, m.loading, m.err = stageCloudflareCookie, false, nil
	m.cookieInput.SetValue("")
	m.userAgentInput.SetValue("")
	for _, credential := range m.sourceCredentials {
		if credential.SourceID == m.selectedSource.ID {
			m.cookieInput.SetValue(credential.Cookie)
			m.userAgentInput.SetValue(credential.UserAgent)
			break
		}
	}
	m.cookieInput.Focus()
	return m, textinput.Blink
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg { return browserOpenedMsg{err: browser.Open(url)} }
}

func (m Model) applyCloudflareCredentials() tea.Cmd {
	credential := appconfig.SourceCredential{
		SourceID: m.selectedSource.ID, URL: m.selectedSource.BaseURL,
		Cookie: strings.TrimSpace(m.cookieInput.Value()), UserAgent: strings.TrimSpace(m.userAgentInput.Value()),
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.service.SetCookies(ctx, credential.URL, credential.Cookie); err != nil {
			return cloudflareAppliedMsg{err: fmt.Errorf("apply cookies: %w", err)}
		}
		if err := m.service.SetUserAgent(ctx, credential.URL, credential.UserAgent); err != nil {
			return cloudflareAppliedMsg{err: fmt.Errorf("apply User-Agent: %w", err)}
		}
		if err := m.configStore.SaveSourceCredential(credential); err != nil {
			return cloudflareAppliedMsg{err: fmt.Errorf("save Cloudflare credentials: %w", err)}
		}
		return cloudflareAppliedMsg{credential: credential}
	}
}

func (m Model) retryCloudflareOperation() tea.Cmd {
	m.loading = true
	switch m.cloudflareOperation {
	case cloudflareSearch:
		return tea.Batch(m.spinner.Tick, m.runSearch(m.resolutionQueryOrSearch()))
	case cloudflareDetails:
		return tea.Batch(m.spinner.Tick, m.loadDetails())
	case cloudflareVideos:
		return tea.Batch(m.spinner.Tick, m.loadVideos())
	case cloudflarePages:
		return tea.Batch(m.spinner.Tick, m.loadPages())
	default:
		return nil
	}
}

func (m Model) resolutionQueryOrSearch() string {
	if m.resolvingEpisode != nil && m.resolutionQuery != "" {
		return m.resolutionQuery
	}
	return strings.TrimSpace(m.search.Value())
}

func prependSourceCredential(items []appconfig.SourceCredential, credential appconfig.SourceCredential) []appconfig.SourceCredential {
	result := []appconfig.SourceCredential{credential}
	for _, existing := range items {
		if existing.SourceID != credential.SourceID {
			result = append(result, existing)
		}
	}
	return result
}

func (m Model) loadDetails() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		media, err := m.service.Details(ctx, m.selectedSource, m.selectedMedia)
		malID := 0
		if m.resolvingEpisode != nil {
			malID = m.resolvingEpisode.MALID
		}
		if err == nil && m.resolvingEpisode != nil && m.resolvingEpisode.MediaID != 0 {
			catalogMetadata, metadataErr := m.anilistClient.EpisodeMetadata(ctx, m.resolvingEpisode.MediaID)
			if metadataErr == nil {
				if malID == 0 {
					malID = catalogMetadata.MALID
				}
				for index := range media.Episodes {
					number := normalizedEpisodeNumber(media.Episodes[index].Number())
					if episodeMetadata, ok := catalogMetadata.Episodes[number]; ok {
						media.Episodes[index].Title = episodeMetadata.Title
						media.Episodes[index].Description = episodeMetadata.Description
						if media.Episodes[index].Thumbnail == "" {
							media.Episodes[index].Thumbnail = episodeMetadata.Image
						}
					}
				}
			}
		}
		return detailsMsg{media: media, malID: malID, err: err}
	}
}

func (m Model) loadVideos() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		items, err := m.service.Videos(ctx, m.selectedSource, m.selectedEpisode)
		return videosMsg{items: items, err: err}
	}
}

func (m Model) loadPages() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), extensionRequestTimeout)
		defer cancel()
		items, err := m.service.Pages(ctx, m.selectedSource, m.selectedEpisode)
		return pagesMsg{items: items, err: err}
	}
}
