package source

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseImageRef(t *testing.T) {
	cases := []struct{ in, host, repo string }{
		{"quay.io/minio/minio", "quay.io", "minio/minio"},
		{"ghcr.io/argoproj/argo-cd", "ghcr.io", "argoproj/argo-cd"},
		{"nginx", "docker.io", "library/nginx"},
		{"library/nginx", "docker.io", "library/nginx"},
		{"docker.io/grafana/grafana", "docker.io", "grafana/grafana"},
		{"registry.example.com:5000/team/app", "registry.example.com:5000", "team/app"},
		{"quay.io/minio/minio:latest", "quay.io", "minio/minio"},
	}
	for _, c := range cases {
		host, repo, err := parseImageRef(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if host != c.host || repo != c.repo {
			t.Errorf("%s => host=%q repo=%q, want host=%q repo=%q", c.in, host, repo, c.host, c.repo)
		}
	}
}

func TestParseBearerChallenge(t *testing.T) {
	p := parseBearerChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)
	if p["realm"] != "https://auth.docker.io/token" || p["service"] != "registry.docker.io" || p["scope"] != "repository:library/nginx:pull" {
		t.Fatalf("bad parse: %#v", p)
	}
}

// mockRT routes requests by URL in-process, so the OCI client can be tested
// without any TCP sockets (this environment has no working loopback).
type mockRT struct {
	fn func(*http.Request) *http.Response
}

func (m mockRT) RoundTrip(r *http.Request) (*http.Response, error) { return m.fn(r), nil }

func mkResp(status int, hdr http.Header, body string) *http.Response {
	if hdr == nil {
		hdr = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestOCITokenDanceAndPagination verifies: a 401 triggers exactly one token
// fetch, the bearer token is then attached, and Link-header pagination is
// followed to completion.
func TestOCITokenDanceAndPagination(t *testing.T) {
	var tokenHits, page1, page2 int

	rt := mockRT{fn: func(r *http.Request) *http.Response {
		switch {
		case r.URL.Path == "/token":
			tokenHits++
			if r.URL.Query().Get("scope") == "" {
				t.Errorf("token request missing scope")
			}
			return mkResp(200, nil, `{"token":"abc123"}`)

		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			if r.Header.Get("Authorization") != "Bearer abc123" {
				h := http.Header{}
				h.Set("WWW-Authenticate", `Bearer realm="https://reg.test/token",service="reg",scope="repository:team/app:pull"`)
				return mkResp(401, h, `{"errors":[]}`)
			}
			if r.URL.Query().Get("page") == "2" {
				page2++
				return mkResp(200, nil, `{"name":"team/app","tags":["v1.1.0"]}`)
			}
			page1++
			h := http.Header{}
			h.Set("Link", `</v2/team/app/tags/list?page=2>; rel="next"`)
			return mkResp(200, h, `{"name":"team/app","tags":["v1.0.0"]}`)
		}
		return mkResp(404, nil, "not found: "+r.URL.Path)
	}}

	o := &ociSource{
		host:     "reg.test",
		endpoint: "reg.test",
		repo:     "team/app",
		hc:       &http.Client{Transport: rt},
	}

	tags, err := o.ListTags(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, tg := range tags {
		got = append(got, tg.Name)
	}
	if len(got) != 2 || got[0] != "v1.0.0" || got[1] != "v1.1.0" {
		t.Fatalf("tags = %v, want [v1.0.0 v1.1.0]", got)
	}
	if tokenHits != 1 {
		t.Fatalf("expected exactly 1 token fetch, got %d", tokenHits)
	}
	if page1 == 0 || page2 == 0 {
		t.Fatalf("expected both pages fetched (p1=%d p2=%d)", page1, page2)
	}
}
