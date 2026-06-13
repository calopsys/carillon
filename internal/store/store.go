// Package store persists the per-tracker high-water mark in Redis. A no-op
// implementation backs stateless mode (no Redis configured) and --dry-run, where
// every run behaves as a first run and nothing is written.
package store

import "context"

// Mark is the stored state for one tracker, keyed by Name. Source/Identifier are
// informational (refreshed on every write, for human inspection of the raw key);
// Tag and Key are the high-water mark the orchestrator compares against; Spec is
// a fingerprint of the comparison basis (regex + compare groups) that produced
// Key, so a pattern change re-baselines instead of comparing incommensurable keys.
type Mark struct {
	Name       string
	Source     string
	Identifier string
	Tag        string
	Key        []int64
	Spec       string
}

// Store reads and writes high-water marks.
type Store interface {
	// Get returns the stored mark for name; found is false on first sighting.
	Get(ctx context.Context, name string) (m Mark, found bool, err error)
	// Upsert writes (insert or update) the mark for m.Name.
	Upsert(ctx context.Context, m Mark) error
	// Close releases resources.
	Close()
}

// NoOp is a stateless Store: Get never finds anything, Upsert discards.
type NoOp struct{}

func (NoOp) Get(context.Context, string) (Mark, bool, error) { return Mark{}, false, nil }
func (NoOp) Upsert(context.Context, Mark) error              { return nil }
func (NoOp) Close()                                          {}
