package source

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/calopsys/carillon/internal/config"
)

type githubSource struct {
	repo    string
	apiBase string // e.g. https://api.github.com
	webBase string // e.g. https://github.com
	ref     string // tags | releases
	token   string
	hc      *http.Client
}

func newGitHub(t config.Track, cred string, hc *http.Client) *githubSource {
	apiBase := "https://api.github.com"
	webBase := "https://github.com"
	if t.BaseURL != "" {
		base := strings.TrimRight(t.BaseURL, "/")
		apiBase = base + "/api/v3"
		webBase = base
	}
	return &githubSource{
		repo:    t.Repo,
		apiBase: apiBase,
		webBase: webBase,
		ref:     t.EffectiveRef(),
		token:   cred,
		hc:      hc,
	}
}

func (g *githubSource) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
}

func (g *githubSource) ListTags(ctx context.Context) ([]Tag, error) {
	if g.ref == config.RefReleases {
		return g.listReleases(ctx)
	}
	return g.listTags(ctx)
}

func (g *githubSource) listTags(ctx context.Context) ([]Tag, error) {
	url := g.apiBase + "/repos/" + g.repo + "/tags?per_page=100"
	type ghTag struct {
		Name string `json:"name"`
	}
	return fetchPaged(ctx, g.hc, url, g.setHeaders, func(page []ghTag) []Tag {
		out := make([]Tag, 0, len(page))
		for _, t := range page {
			out = append(out, Tag{
				Name: t.Name,
				URL:  g.webBase + "/" + g.repo + "/releases/tag/" + t.Name,
			})
		}
		return out
	})
}

func (g *githubSource) listReleases(ctx context.Context) ([]Tag, error) {
	url := g.apiBase + "/repos/" + g.repo + "/releases?per_page=100"
	type ghRelease struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		PublishedAt time.Time `json:"published_at"`
	}
	return fetchPaged(ctx, g.hc, url, g.setHeaders, func(page []ghRelease) []Tag {
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
