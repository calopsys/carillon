// Package notify delivers new-release events, either to Mattermost (templated
// attachment card) or, when notification is not configured / in --dry-run, to
// the structured log (log-only mode).
package notify

import "context"

// Event describes a newly discovered release to announce.
type Event struct {
	Name       string // tracker name
	Source     string // github | gitlab | oci
	Identifier string // repo or image ref
	Version    string // the new tag
	Previous   string // prior tag ("" on first sighting)
	URL        string // best-effort web link
	Body       string // release notes (releases mode), may be empty
	FirstSeen  bool   // true when this is the tracker's first run
}

// Notifier delivers an Event. webhook is the resolved destination key (a key in
// the configured webhooks); the caller resolves any default. Implementations
// that don't route (log-only) ignore it. ctx carries the run's cancellation.
type Notifier interface {
	Notify(ctx context.Context, webhook string, e Event) error
}
