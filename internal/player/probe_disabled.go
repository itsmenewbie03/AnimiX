//go:build !probe

package player

func openPlaybackProbe() (playbackProbe, error) { return nil, nil }
