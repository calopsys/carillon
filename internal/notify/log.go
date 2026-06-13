package notify

import (
	"context"
	"log/slog"
)

// LogNotifier implements log-only mode: each event is emitted as a structured
// log line instead of being posted to Mattermost.
type LogNotifier struct {
	Logger *slog.Logger
}

func (l LogNotifier) Notify(_ context.Context, _ string, e Event) error {
	log := l.Logger
	if log == nil {
		log = slog.Default()
	}
	log.Info("new release",
		"tracker", e.Name,
		"source", e.Source,
		"identifier", e.Identifier,
		"version", e.Version,
		"previous", e.Previous,
		"first_seen", e.FirstSeen,
		"url", e.URL,
	)
	return nil
}
