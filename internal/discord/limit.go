package discord

import "time"

// rollingLimit is owned by the Gateway worker. Its history survives reconnects.
type rollingLimit struct {
	window time.Duration
	sent   []time.Time
}

func (l *rollingLimit) next(now time.Time, maximum int) time.Time {
	for len(l.sent) > 0 && !l.sent[0].Add(l.window).After(now) {
		l.sent = l.sent[1:]
	}
	if len(l.sent) >= maximum {
		return l.sent[len(l.sent)-maximum].Add(l.window)
	}
	return now
}

func (l *rollingLimit) record(now time.Time) { l.sent = append(l.sent, now) }
