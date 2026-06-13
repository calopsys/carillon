// Package run is the orchestrator: for each tracked artifact it fetches tags,
// computes the latest matching version, compares against stored state, and (on a
// newer version) notifies then persists — in that order (send-then-persist), so
// a delivery failure is retried next run rather than silently lost. Trackers are
// processed concurrently and in isolation: one failure never sinks the others.
package run

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/calopsys/carillon/internal/config"
	"github.com/calopsys/carillon/internal/notify"
	"github.com/calopsys/carillon/internal/source"
	"github.com/calopsys/carillon/internal/store"
	"github.com/calopsys/carillon/internal/version"

	"golang.org/x/sync/errgroup"
)

// Deps are the collaborators a run needs.
type Deps struct {
	Store    store.Store
	Notifier notify.Notifier
	Creds    map[string]string // credential name -> resolved value
	HTTP     *http.Client
	Logger   *slog.Logger
	// NewSource builds the Source for a track; defaults to source.New. Overridden
	// in tests to inject fakes.
	NewSource func(config.Track, string, *http.Client) (source.Source, error)
}

// Result summarizes a run.
type Result struct {
	Notified int
	UpToDate int
	NoMatch  int
	Errors   int
}

// Run processes every track in cfg, bounded by cfg.Concurrency. It returns a
// Result; per-tracker failures are counted in Result.Errors (and logged), not
// returned as the function error.
func Run(ctx context.Context, cfg *config.Config, deps Deps) Result {
	if deps.HTTP == nil {
		deps.HTTP = source.DefaultClient()
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.NewSource == nil {
		deps.NewSource = source.New
	}

	var (
		mu  sync.Mutex
		res Result
	)
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Concurrency)

	for _, t := range cfg.Track {
		g.Go(func() error {
			outcome, err := processTrackSafe(ctx, cfg, t, deps)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				res.Errors++
				deps.Logger.Error("tracker failed", "tracker", t.Name, "err", err.Error())
			case outcome == outcomeNotified:
				res.Notified++
			case outcome == outcomeUpToDate:
				res.UpToDate++
			case outcome == outcomeNoMatch:
				res.NoMatch++
			}
			return nil // isolate: never cancel siblings
		})
	}
	_ = g.Wait()

	deps.Logger.Info("run complete",
		"trackers", len(cfg.Track),
		"notified", res.Notified,
		"up_to_date", res.UpToDate,
		"no_match", res.NoMatch,
		"errors", res.Errors,
	)
	return res
}

type outcome int

const (
	outcomeNotified outcome = iota
	outcomeUpToDate
	outcomeNoMatch
)

// processTrackSafe runs processTrack and converts a panic into an error so one
// misbehaving tracker can't crash the whole run (per-tracker isolation).
func processTrackSafe(ctx context.Context, cfg *config.Config, t config.Track, deps Deps) (oc outcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			deps.Logger.Error("tracker panicked",
				"tracker", t.Name, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return processTrack(ctx, cfg, t, deps)
}

func processTrack(ctx context.Context, cfg *config.Config, t config.Track, deps Deps) (outcome, error) {
	cred := deps.Creds[t.Credential]
	src, err := deps.NewSource(t, cred, deps.HTTP)
	if err != nil {
		return 0, err
	}
	tags, err := src.ListTags(ctx)
	if err != nil {
		return 0, err
	}

	regex, compare, err := cfg.ResolvePattern(t)
	if err != nil {
		return 0, err
	}
	spec, err := version.Compile(regex, compare)
	if err != nil {
		return 0, err
	}

	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}

	latest, ok := spec.Latest(names)
	if !ok {
		deps.Logger.Info("no matching tags", "tracker", t.Name, "candidates", len(tags))
		return outcomeNoMatch, nil
	}

	// Recover the winning tag's metadata by name (avoids building a map over
	// every tag, which would copy each release Body — MBs in releases mode).
	var winner source.Tag
	for _, c := range tags {
		if c.Name == latest.Tag {
			winner = c
			break
		}
	}

	mark, found, err := deps.Store.Get(ctx, t.Name)
	if err != nil {
		return 0, err
	}

	fingerprint := spec.Fingerprint()
	firstSeen := true
	previous := ""
	if found {
		// Only compare keys built from the same comparison basis. Any pattern
		// change (and thus any arity change) shifts the fingerprint, so the
		// stored key is incommensurable — re-baseline instead of comparing it.
		if mark.Spec == fingerprint {
			if version.CompareKeys(latest.Key, mark.Key) <= 0 {
				return outcomeUpToDate, nil
			}
			firstSeen = false
			previous = mark.Tag
		} else {
			deps.Logger.Warn("comparison basis changed; re-baselining",
				"tracker", t.Name, "stored_tag", mark.Tag)
		}
	}

	ev := notify.Event{
		Name:       t.Name,
		Source:     t.Source,
		Identifier: t.Identifier(),
		Version:    latest.Tag,
		Previous:   previous,
		URL:        winner.URL,
		Body:       winner.Body,
		FirstSeen:  firstSeen,
	}

	// Send first; persist only if delivery succeeded (at-least-once). The
	// webhook destination is resolved here (track override or default) so the
	// notifier never re-implements routing.
	if err := deps.Notifier.Notify(ctx, cfg.WebhookName(t), ev); err != nil {
		return 0, err
	}
	if err := deps.Store.Upsert(ctx, store.Mark{
		Name:       t.Name,
		Source:     t.Source,
		Identifier: t.Identifier(),
		Tag:        latest.Tag,
		Key:        latest.Key,
		Spec:       fingerprint,
	}); err != nil {
		// Notification already went out; surface the persistence failure so the
		// operator knows the mark didn't advance (next run may re-notify).
		return 0, err
	}

	deps.Logger.Info("notified new release",
		"tracker", t.Name, "version", latest.Tag, "previous", previous, "first_seen", firstSeen)
	return outcomeNotified, nil
}
