package tui

import (
	"time"

	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/discord"
	"github.com/itsmenewbie03/animix/internal/mangareader"
)

func mangaSnapshot(entry appconfig.MangaReadingEntry) discord.Snapshot {
	return discord.Snapshot{
		Kind: discord.Reading, Title: entry.Title, Part: entry.ChapterName, Number: entry.ChapterNumber,
		AniListMediaID: entry.AniListMediaID, CoverURL: entry.CoverURL, Page: entry.Page, Pages: entry.TotalPages,
	}
}

func (m *Model) beginReaderPresence(id string, entry appconfig.MangaReadingEntry) {
	if m.discord != nil {
		m.mangaPresence[id] = m.discord.Begin(mangaSnapshot(entry))
	}
}

func (m *Model) updateReaderPresence(event mangareader.ProgressEvent, entry appconfig.MangaReadingEntry) {
	if m.discord == nil || event.SessionID != m.activeMangaSessionID || event.SeenAt.IsZero() {
		return
	}
	snapshot := mangaSnapshot(entry)
	snapshot.Active = event.Visible
	snapshot.ObservedAt, snapshot.ValidUntil = event.SeenAt, event.SeenAt.Add(time.Minute)
	m.discord.Update(m.mangaPresence[event.SessionID], snapshot)
}

func (m *Model) endReaderPresence(id string) {
	if m.discord != nil {
		m.discord.End(m.mangaPresence[id])
	}
	delete(m.mangaPresence, id)
}
