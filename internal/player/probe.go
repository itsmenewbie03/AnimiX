package player

import "io"

// playbackProbe is implemented only by probe builds. The normal build binds a
// no-op factory so production playback has no diagnostic file or extra stderr
// processing.
type playbackProbe interface {
	record(string, map[string]any)
	stderr() io.Writer
	path() string
	close() error
}
