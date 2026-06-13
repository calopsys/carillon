package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func startsWithReleaseEmoji(title string) bool {
	for _, e := range releaseEmojis {
		if strings.HasPrefix(title, e+" ") {
			return true
		}
	}
	return false
}

type capRT struct {
	gotURL  string
	gotBody []byte
	status  int
}

func (c *capRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.gotURL = r.URL.String()
	c.gotBody, _ = io.ReadAll(r.Body)
	st := c.status
	if st == 0 {
		st = 200
	}
	return &http.Response{StatusCode: st, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestMattermostPayloadAndRouting(t *testing.T) {
	rt := &capRT{}
	hooks := map[string]string{"default": "https://mm/hooks/default", "sec": "https://mm/hooks/sec"}
	mm, err := NewMattermost(hooks, "", &http.Client{Transport: rt})
	if err != nil {
		t.Fatal(err)
	}

	ev := Event{Name: "traefik", Source: "github", Identifier: "traefik/traefik",
		Version: "v3.2.0", Previous: "v3.1.5", URL: "https://example/v3.2.0"}

	// per-tracker override routes to "sec"
	if err := mm.Notify(t.Context(), "sec", ev); err != nil {
		t.Fatal(err)
	}
	if rt.gotURL != "https://mm/hooks/sec" {
		t.Fatalf("routed to %q, want sec hook", rt.gotURL)
	}

	var payload struct {
		Attachments []struct {
			Title     string `json:"title"`
			TitleLink string `json:"title_link"`
			Text      string `json:"text"`
			Fields    []struct {
				Title, Value string
			} `json:"fields"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(rt.gotBody, &payload); err != nil {
		t.Fatalf("payload not JSON: %v (%s)", err, rt.gotBody)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("want 1 attachment, got %d", len(payload.Attachments))
	}
	a := payload.Attachments[0]
	if !strings.HasSuffix(a.Title, "traefik — v3.2.0") || a.TitleLink != "https://example/v3.2.0" {
		t.Fatalf("bad title/link: %+v", a)
	}
	if !startsWithReleaseEmoji(a.Title) {
		t.Fatalf("title missing emoji prefix: %q", a.Title)
	}
	if !strings.Contains(a.Text, "v3.1.5") || !strings.Contains(a.Text, "v3.2.0") {
		t.Fatalf("text missing version transition: %q", a.Text)
	}
	var hasPrev bool
	for _, f := range a.Fields {
		if f.Title == "Previous" && f.Value == "v3.1.5" {
			hasPrev = true
		}
	}
	if !hasPrev {
		t.Fatalf("missing Previous field: %+v", a.Fields)
	}

	// the caller resolves the default to a concrete key before calling Notify
	if err := mm.Notify(t.Context(), "default", ev); err != nil {
		t.Fatal(err)
	}
	if rt.gotURL != "https://mm/hooks/default" {
		t.Fatalf("default routed to %q", rt.gotURL)
	}
}

func TestMattermostNon2xxIsError(t *testing.T) {
	rt := &capRT{status: 500}
	mm, _ := NewMattermost(map[string]string{"d": "https://mm/d"}, "", &http.Client{Transport: rt})
	if err := mm.Notify(t.Context(), "d", Event{Name: "x", Version: "v1"}); err == nil {
		t.Fatal("expected error on 500 so the mark is not persisted")
	}
}

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	// "é" is 2 bytes; a budget of 3 would split the second one mid-rune.
	s := "aéé"
	got := truncateBytes(s, 3)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateBytes produced invalid UTF-8: %q", got)
	}
	if got != "aé" {
		t.Fatalf("want %q, got %q", "aé", got)
	}
	if truncateBytes("abc", 10) != "abc" {
		t.Fatal("strings within budget must pass through unchanged")
	}
}

func TestMattermostUnknownWebhook(t *testing.T) {
	mm, _ := NewMattermost(map[string]string{"d": "https://mm/d"}, "", &http.Client{Transport: &capRT{}})
	if err := mm.Notify(t.Context(), "nope", Event{Name: "x", Version: "v1"}); err == nil {
		t.Fatal("expected error for unknown webhook name")
	}
}
