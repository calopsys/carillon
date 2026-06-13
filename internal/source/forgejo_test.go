package source

import (
	"net/http"
	"strings"
	"testing"

	"github.com/calopsys/carillon/internal/config"
)

// TestForgejoTagsPaginationAndAuth verifies tags-mode listing follows rel="next"
// Link pagination to completion and attaches the `Authorization: token` header.
// The default host (codeberg.org) is exercised by leaving base_url unset; the
// mock RoundTripper routes by path/query, so no TCP socket is used.
func TestForgejoTagsPaginationAndAuth(t *testing.T) {
	var page1, page2 int
	rt := mockRT{fn: func(r *http.Request) *http.Response {
		if r.Header.Get("Authorization") != "token secret" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/repos/owner/app/tags") {
			return mkResp(404, nil, "not found: "+r.URL.Path)
		}
		if r.URL.Query().Get("page") == "2" {
			page2++
			return mkResp(200, nil, `[{"name":"v1.1.0"}]`)
		}
		page1++
		h := http.Header{}
		h.Set("Link", `<https://codeberg.org/api/v1/repos/owner/app/tags?limit=100&page=2>; rel="next"`)
		return mkResp(200, h, `[{"name":"v1.0.0"}]`)
	}}

	g := newForgejo(
		config.Track{Source: config.SourceForgejo, Repo: "owner/app"},
		"secret",
		&http.Client{Transport: rt},
	)

	tags, err := g.ListTags(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "v1.0.0" || tags[1].Name != "v1.1.0" {
		t.Fatalf("tags = %+v, want v1.0.0 then v1.1.0", tags)
	}
	if tags[0].URL != "https://codeberg.org/owner/app/src/tag/v1.0.0" {
		t.Fatalf("tag URL = %q, want codeberg src/tag link", tags[0].URL)
	}
	if page1 == 0 || page2 == 0 {
		t.Fatalf("expected both pages fetched (p1=%d p2=%d)", page1, page2)
	}
}

// TestForgejoReleasesSkipsDraftAnonymous verifies releases-mode skips drafts and
// sends no Authorization header when no credential is configured. A self-hosted
// base_url is exercised here.
func TestForgejoReleasesSkipsDraftAnonymous(t *testing.T) {
	rt := mockRT{fn: func(r *http.Request) *http.Response {
		if _, ok := r.Header["Authorization"]; ok {
			t.Errorf("anonymous request must not set Authorization")
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/repos/team/tool/releases") {
			return mkResp(404, nil, "not found: "+r.URL.Path)
		}
		return mkResp(200, nil, `[
			{"tag_name":"v2.0.0","draft":false,"body":"notes","html_url":"https://git.example.com/team/tool/releases/tag/v2.0.0"},
			{"tag_name":"v2.1.0","draft":true,"html_url":"https://git.example.com/team/tool/releases/tag/v2.1.0"}
		]`)
	}}

	g := newForgejo(
		config.Track{Source: config.SourceForgejo, Repo: "team/tool", BaseURL: "https://git.example.com/", Ref: config.RefReleases},
		"",
		&http.Client{Transport: rt},
	)

	tags, err := g.ListTags(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v2.0.0" {
		t.Fatalf("tags = %+v, want only non-draft v2.0.0", tags)
	}
	if tags[0].URL != "https://git.example.com/team/tool/releases/tag/v2.0.0" || tags[0].Body != "notes" {
		t.Fatalf("release metadata not mapped: %+v", tags[0])
	}
}
