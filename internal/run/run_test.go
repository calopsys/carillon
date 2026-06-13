package run

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/calopsys/carillon/internal/config"
	"github.com/calopsys/carillon/internal/notify"
	"github.com/calopsys/carillon/internal/source"
	"github.com/calopsys/carillon/internal/store"
	"github.com/calopsys/carillon/internal/version"
)

const semverRegex = `^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$`

// semverFingerprint is the comparison-basis fingerprint a stored mark must carry
// to be compared (rather than re-baselined) against the testCfg semver pattern.
func semverFingerprint(t *testing.T) string {
	t.Helper()
	spec, err := version.Compile(semverRegex, []string{"major", "minor", "patch"})
	if err != nil {
		t.Fatal(err)
	}
	return spec.Fingerprint()
}

type fakeSource struct{ tags []source.Tag }

func (f fakeSource) ListTags(context.Context) ([]source.Tag, error) { return f.tags, nil }

type panicSource struct{}

func (panicSource) ListTags(context.Context) ([]source.Tag, error) { panic("boom") }

type fakeStore struct {
	mu      sync.Mutex
	marks   map[string]store.Mark
	upserts int
}

func newFakeStore() *fakeStore { return &fakeStore{marks: map[string]store.Mark{}} }

func (s *fakeStore) Get(_ context.Context, name string) (store.Mark, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.marks[name]
	return m, ok, nil
}
func (s *fakeStore) Upsert(_ context.Context, m store.Mark) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marks[m.Name] = m
	s.upserts++
	return nil
}
func (s *fakeStore) Close() {}

type fakeNotifier struct {
	mu     sync.Mutex
	events []notify.Event
	fail   bool
}

func (n *fakeNotifier) Notify(_ context.Context, _ string, e notify.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fail {
		return errors.New("webhook down")
	}
	n.events = append(n.events, e)
	return nil
}

func testCfg(tags ...string) (*config.Config, func(config.Track, string, *http.Client) (source.Source, error)) {
	cfg := &config.Config{
		Concurrency: 2,
		Patterns: map[string]config.Pattern{
			"semver": {Regex: semverRegex, Compare: []string{"major", "minor", "patch"}},
		},
		Track: []config.Track{{Name: "x", Source: "github", Repo: "o/r", Pattern: "semver"}},
	}
	st := make([]source.Tag, len(tags))
	for i, t := range tags {
		st[i] = source.Tag{Name: t, URL: "https://example/" + t}
	}
	factory := func(config.Track, string, *http.Client) (source.Source, error) {
		return fakeSource{tags: st}, nil
	}
	return cfg, factory
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFirstRunNotifiesAndPersists(t *testing.T) {
	cfg, factory := testCfg("v1.0.0", "v1.2.0", "v1.1.0", "latest")
	st := newFakeStore()
	nf := &fakeNotifier{}
	res := Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if res.Notified != 1 {
		t.Fatalf("want 1 notified, got %+v", res)
	}
	if len(nf.events) != 1 || nf.events[0].Version != "v1.2.0" || !nf.events[0].FirstSeen {
		t.Fatalf("bad event: %+v", nf.events)
	}
	if m := st.marks["x"]; m.Tag != "v1.2.0" || m.Spec != semverFingerprint(t) {
		t.Fatalf("mark not persisted at v1.2.0 with spec fingerprint: %+v", m)
	}
}

func TestUpToDateDoesNotNotify(t *testing.T) {
	cfg, factory := testCfg("v1.2.0", "v1.1.0")
	st := newFakeStore()
	st.marks["x"] = store.Mark{Name: "x", Tag: "v1.2.0", Key: []int64{1, 2, 0}, Spec: semverFingerprint(t)}
	nf := &fakeNotifier{}
	res := Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if res.UpToDate != 1 || res.Notified != 0 {
		t.Fatalf("want up-to-date, got %+v", res)
	}
	if len(nf.events) != 0 || st.upserts != 0 {
		t.Fatalf("should not notify or persist: events=%d upserts=%d", len(nf.events), st.upserts)
	}
}

func TestCatchUpNotifiesOnlyNewest(t *testing.T) {
	cfg, factory := testCfg("v1.0.1", "v1.0.2", "v1.0.3")
	st := newFakeStore()
	st.marks["x"] = store.Mark{Name: "x", Tag: "v1.0.0", Key: []int64{1, 0, 0}, Spec: semverFingerprint(t)}
	nf := &fakeNotifier{}
	Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if len(nf.events) != 1 || nf.events[0].Version != "v1.0.3" {
		t.Fatalf("want single newest v1.0.3, got %+v", nf.events)
	}
	if nf.events[0].Previous != "v1.0.0" || nf.events[0].FirstSeen {
		t.Fatalf("want previous=v1.0.0 firstSeen=false, got %+v", nf.events[0])
	}
}

func TestSendFailureDoesNotPersist(t *testing.T) {
	cfg, factory := testCfg("v1.0.0")
	st := newFakeStore()
	nf := &fakeNotifier{fail: true}
	res := Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if res.Errors != 1 {
		t.Fatalf("want 1 error, got %+v", res)
	}
	if st.upserts != 0 {
		t.Fatalf("must not persist after failed send, upserts=%d", st.upserts)
	}
}

func TestNoMatch(t *testing.T) {
	cfg, factory := testCfg("latest", "main", "sha-deadbeef")
	st := newFakeStore()
	nf := &fakeNotifier{}
	res := Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if res.NoMatch != 1 || res.Notified != 0 {
		t.Fatalf("want no-match, got %+v", res)
	}
}

func TestPanicIsIsolatedAsError(t *testing.T) {
	cfg, _ := testCfg("v1.0.0")
	factory := func(config.Track, string, *http.Client) (source.Source, error) {
		return panicSource{}, nil
	}
	st := newFakeStore()
	nf := &fakeNotifier{}
	res := Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if res.Errors != 1 {
		t.Fatalf("a panicking tracker must surface as 1 error, not crash the run: %+v", res)
	}
	if st.upserts != 0 {
		t.Fatalf("must not persist after a panic, upserts=%d", st.upserts)
	}
}

func TestRebaselineOnSpecChange(t *testing.T) {
	cfg, factory := testCfg("v1.2.0")
	st := newFakeStore()
	// Same arity (3) but a DIFFERENT comparison basis (foreign fingerprint), and
	// a stored key that would otherwise sort HIGHER than v1.2.0. Under the old
	// arity-only check this compared as "up to date" and suppressed the release;
	// the fingerprint mismatch must instead re-baseline and notify.
	st.marks["x"] = store.Mark{Name: "x", Tag: "2026.6.13", Key: []int64{2026, 6, 13}, Spec: "deadbeefdeadbeef"}
	nf := &fakeNotifier{}
	Run(t.Context(), cfg, Deps{Store: st, Notifier: nf, NewSource: factory, Logger: quietLogger()})

	if len(nf.events) != 1 || !nf.events[0].FirstSeen {
		t.Fatalf("want re-baseline notify with FirstSeen, got %+v", nf.events)
	}
	if m := st.marks["x"]; m.Tag != "v1.2.0" || m.Spec != semverFingerprint(t) {
		t.Fatalf("want mark advanced to v1.2.0 with current spec, got %+v", m)
	}
}
