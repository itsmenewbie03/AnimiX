package catalog

import "encoding/json"

type Source struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"lang"`
	BaseURL  string `json:"baseUrl"`
	Type     string `json:"type"`
	Class    string `json:"className"`
	Package  string `json:"pkgName"`
}

func (s Source) IsAnime() bool { return s.Type == "anime" }

type Pages struct {
	List        []Media `json:"list"`
	HasNextPage bool    `json:"hasNextPage"`
	Error       string  `json:"error"`
}

type Media struct {
	Title       string    `json:"title"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Cover       string    `json:"cover"`
	Thumbnail   string    `json:"thumbnail_url"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Artist      string    `json:"artist"`
	Genres      []string  `json:"genre"`
	Episodes    []Episode `json:"episodes"`
}

func (m Media) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	return m.Name
}

type Episode struct {
	URL           string            `json:"url"`
	Name          string            `json:"name"`
	Title         string            `json:"title"`
	DateUpload    any               `json:"dateUpload"`
	Scanlator     string            `json:"scanlator"`
	Thumbnail     string            `json:"thumbnail"`
	Description   string            `json:"description"`
	Summary       string            `json:"summary"`
	Memo          string            `json:"memo"`
	EpisodeNumber any               `json:"episodeNumber"`
	SortMap       map[string]string `json:"sortMap"`
}

func (e *Episode) UnmarshalJSON(data []byte) error {
	type episodeAlias Episode
	var payload struct {
		episodeAlias
		EpisodeNumberCamel any `json:"episodeNumber"`
		EpisodeNumberSnake any `json:"episode_number"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	*e = Episode(payload.episodeAlias)
	if payload.EpisodeNumberCamel != nil {
		e.EpisodeNumber = payload.EpisodeNumberCamel
	} else {
		e.EpisodeNumber = payload.EpisodeNumberSnake
	}
	return nil
}

func (e Episode) Number() string {
	if e.EpisodeNumber == nil {
		return ""
	}
	switch value := e.EpisodeNumber.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
}

type Video struct {
	Title     string            `json:"title"`
	URL       string            `json:"url"`
	Quality   string            `json:"quality"`
	Headers   map[string]string `json:"headers"`
	Subtitles []Track           `json:"subtitles"`
	Audios    []Track           `json:"audios"`
	ExtraData map[string]any    `json:"extraData"`
}

type Track struct {
	File  string `json:"file"`
	Label string `json:"label"`
}

type Page struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}
