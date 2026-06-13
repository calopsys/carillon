package source

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/calopsys/carillon/internal/config"
)

type forgejoSource struct {
	repo    string
	baseURL string // e.g. https://codeberg.org
	ref     string // tags | releases
	token   string
	hc      *http.Client
}

func newForgejo(t config.Track, cred string, hc *http.Client) *forgejoSource {
	base := "https://codeberg.org"
	if t.BaseURL != "" {
		base = strings.TrimRight(t.BaseURL, "/")
	}
	return &forgejoSource{
		repo:    t.Repo,
		baseURL: base,
		ref:     t.EffectiveRef(),
		token:   cred,
		hc:      hc,
	}
}

func (g *forgejoSource) setHeaders(req *http.Request) {
	if g.token != "" {
		req.Header.Set("Authorization", "token "+g.token)
	}
}

func (g *forgejoSource) apiBase() string {
	return g.baseURL + "/api/v1/repos/" + g.repo
}

func (g *forgejoSource) ListTags(ctx context.Context) ([]Tag, error) {
	if g.ref == config.RefReleases {
		return g.listReleases(ctx)
	}
	return g.listTags(ctx)
}

func (g *forgejoSource) listTags(ctx context.Context) ([]Tag, error) {
	u := g.apiBase() + "/tags?limit=100"
	type fjTag struct {
		Name string `json:"name"`
	}
	return fetchPaged(ctx, g.hc, u, g.setHeaders, func(page []fjTag) []Tag {
		out := make([]Tag, 0, len(page))
		for _, t := range page {
			out = append(out, Tag{
				Name: t.Name,
				URL:  g.baseURL + "/" + g.repo + "/src/tag/" + t.Name,
			})
		}
		return out
	})
}

func (g *forgejoSource) listReleases(ctx context.Context) ([]Tag, error) {
	u := g.apiBase() + "/releases?limit=50"
	type fjRelease struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		PublishedAt time.Time `json:"published_at"`
	}
	return fetchPaged(ctx, g.hc, u, g.setHeaders, func(page []fjRelease) []Tag {
		out := make([]Tag, 0, len(page))
		for _, r := range page {
			if r.Draft || r.TagName == "" {
				continue
			}
			out = append(out, Tag{
				Name:      r.TagName,
				URL:       r.HTMLURL,
				Body:      r.Body,
				Published: r.PublishedAt,
			})
		}
		return out
	})
}
