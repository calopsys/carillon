package source

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/calopsys/carillon/internal/config"
)

type gitlabSource struct {
	project string // raw "group/sub/project" path
	baseURL string // e.g. https://gitlab.com
	ref     string // tags | releases
	token   string
	hc      *http.Client
}

func newGitLab(t config.Track, cred string, hc *http.Client) *gitlabSource {
	base := "https://gitlab.com"
	if t.BaseURL != "" {
		base = strings.TrimRight(t.BaseURL, "/")
	}
	return &gitlabSource{
		project: t.Repo,
		baseURL: base,
		ref:     t.EffectiveRef(),
		token:   cred,
		hc:      hc,
	}
}

func (g *gitlabSource) setHeaders(req *http.Request) {
	if g.token != "" {
		req.Header.Set("Private-Token", g.token)
	}
}

func (g *gitlabSource) projectAPI() string {
	return g.baseURL + "/api/v4/projects/" + url.PathEscape(g.project)
}

func (g *gitlabSource) ListTags(ctx context.Context) ([]Tag, error) {
	if g.ref == config.RefReleases {
		return g.listReleases(ctx)
	}
	return g.listTags(ctx)
}

func (g *gitlabSource) listTags(ctx context.Context) ([]Tag, error) {
	u := g.projectAPI() + "/repository/tags?per_page=100"
	type glTag struct {
		Name string `json:"name"`
	}
	return fetchPaged(ctx, g.hc, u, g.setHeaders, func(page []glTag) []Tag {
		out := make([]Tag, 0, len(page))
		for _, t := range page {
			out = append(out, Tag{
				Name: t.Name,
				URL:  g.baseURL + "/" + g.project + "/-/tags/" + t.Name,
			})
		}
		return out
	})
}

func (g *gitlabSource) listReleases(ctx context.Context) ([]Tag, error) {
	u := g.projectAPI() + "/releases?per_page=100"
	type glRelease struct {
		TagName     string    `json:"tag_name"`
		Description string    `json:"description"`
		ReleasedAt  time.Time `json:"released_at"`
		Links       struct {
			Self string `json:"self"`
		} `json:"_links"`
	}
	return fetchPaged(ctx, g.hc, u, g.setHeaders, func(page []glRelease) []Tag {
		out := make([]Tag, 0, len(page))
		for _, r := range page {
			if r.TagName == "" {
				continue
			}
			link := r.Links.Self
			if link == "" {
				link = g.baseURL + "/" + g.project + "/-/releases/" + r.TagName
			}
			out = append(out, Tag{
				Name:      r.TagName,
				URL:       link,
				Body:      r.Description,
				Published: r.ReleasedAt,
			})
		}
		return out
	})
}
