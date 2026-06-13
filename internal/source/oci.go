package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/calopsys/carillon/internal/config"
)

// dockerHubHost is the canonical registry host assumed when an image reference
// names no registry (e.g. "redis" -> docker.io/library/redis).
const dockerHubHost = "docker.io"

type ociSource struct {
	host     string // canonical host, e.g. docker.io, ghcr.io, quay.io
	endpoint string // registry API host, e.g. registry-1.docker.io
	repo     string // repository path, e.g. library/nginx, minio/minio
	cred     string // "user:pass" or "" for anonymous
	hc       *http.Client
	token    string // cached bearer for this run
}

func newOCI(t config.Track, cred string, hc *http.Client) (*ociSource, error) {
	host, repo, err := parseImageRef(t.Image)
	if err != nil {
		return nil, err
	}
	endpoint := host
	if host == dockerHubHost {
		endpoint = "registry-1.docker.io"
	}
	return &ociSource{host: host, endpoint: endpoint, repo: repo, cred: cred, hc: hc}, nil
}

// parseImageRef splits an image reference into a canonical registry host and a
// repository path, applying Docker's defaulting rules.
func parseImageRef(ref string) (host, repo string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("empty image reference")
	}
	// Strip any :tag or @digest — we list all tags ourselves.
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	first, rest, _ := strings.Cut(ref, "/")
	if isRegistryHost(first) && rest != "" {
		host, repo = first, rest
	} else {
		host, repo = dockerHubHost, ref
	}
	if host == dockerHubHost && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	// drop a trailing :tag on the repo's last segment if present
	if i := strings.LastIndexByte(repo, ':'); i > strings.LastIndexByte(repo, '/') {
		repo = repo[:i]
	}
	return host, repo, nil
}

func isRegistryHost(s string) bool {
	return strings.ContainsAny(s, ".:") || s == "localhost"
}

func (o *ociSource) ListTags(ctx context.Context) ([]Tag, error) {
	u := "https://" + o.endpoint + "/v2/" + o.repo + "/tags/list?n=100"
	var names []string
	for u != "" {
		var body struct {
			Tags []string `json:"tags"`
		}
		hdr, err := o.get(ctx, u, &body)
		if err != nil {
			return nil, err
		}
		names = append(names, body.Tags...)
		u, err = resolveNext(u, hdr)
		if err != nil {
			return nil, err
		}
	}
	out := make([]Tag, 0, len(names))
	for _, n := range names {
		out = append(out, Tag{Name: n, URL: o.webURL(n)})
	}
	return out, nil
}

// get issues a registry GET, transparently performing the bearer-token dance on
// a 401 (once), then retrying transient failures via doJSON for the happy path.
func (o *ociSource) get(ctx context.Context, url string, out any) (http.Header, error) {
	hdr, err := doJSON(ctx, o.hc, url, o.authHeader, out)
	if err == nil {
		return hdr, nil
	}
	var he *httpError
	if !asHTTPError(err, &he) || he.status != http.StatusUnauthorized {
		return nil, err
	}
	// Acquire a token from the challenge and retry once.
	if tErr := o.authenticate(ctx, hdr.Get("WWW-Authenticate")); tErr != nil {
		return nil, fmt.Errorf("oci auth: %w", tErr)
	}
	return doJSON(ctx, o.hc, url, o.authHeader, out)
}

func (o *ociSource) authHeader(req *http.Request) {
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
}

// authenticate parses a `Bearer realm=...,service=...,scope=...` challenge,
// fetches a pull-scoped token (with Basic auth if a credential is configured),
// and caches it on the source.
func (o *ociSource) authenticate(ctx context.Context, challenge string) error {
	params := parseBearerChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return fmt.Errorf("no realm in challenge %q", challenge)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + o.repo + ":pull"
	}
	// Build the token URL via net/url so scope/service are query-escaped and any
	// query already present on the realm is preserved rather than clobbered.
	realmURL, err := url.Parse(realm)
	if err != nil {
		return fmt.Errorf("bad realm %q: %w", realm, err)
	}
	q := realmURL.Query()
	q.Set("scope", scope)
	if svc := params["service"]; svc != "" {
		q.Set("service", svc)
	}
	realmURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realmURL.String(), nil)
	if err != nil {
		return err
	}
	if o.cred != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(o.cred)))
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return err
	}
	o.token = tok.Token
	if o.token == "" {
		o.token = tok.AccessToken
	}
	if o.token == "" {
		return fmt.Errorf("token endpoint returned no token")
	}
	return nil
}

// parseBearerChallenge parses the comma-separated key="value" parameters of a
// `WWW-Authenticate: Bearer ...` header.
func parseBearerChallenge(h string) map[string]string {
	out := map[string]string{}
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "Bearer ")
	for _, part := range splitTopLevel(h) {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		out[strings.TrimSpace(kv[0])] = strings.Trim(strings.TrimSpace(kv[1]), `"`)
	}
	return out
}

// splitTopLevel splits on commas that are not inside double quotes.
func splitTopLevel(s string) []string {
	var parts []string
	var b strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
			b.WriteRune(r)
		case ',':
			if inQuote {
				b.WriteRune(r)
			} else {
				parts = append(parts, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

// webURL returns a best-effort browser link to a tag, or "" if unknown.
func (o *ociSource) webURL(tag string) string {
	switch o.host {
	case "ghcr.io":
		// repo = owner/name(/...) -> last segment is the package name
		owner := o.repo
		name := o.repo
		if i := strings.IndexByte(o.repo, '/'); i >= 0 {
			owner = o.repo[:i]
			name = o.repo[strings.LastIndexByte(o.repo, '/')+1:]
		}
		return "https://github.com/" + owner + "/pkgs/container/" + name
	case "quay.io":
		return "https://quay.io/repository/" + o.repo + "?tab=tags&tag=" + tag
	case dockerHubHost:
		if name, ok := strings.CutPrefix(o.repo, "library/"); ok {
			return "https://hub.docker.com/_/" + name + "/tags"
		}
		return "https://hub.docker.com/r/" + o.repo + "/tags"
	default:
		return ""
	}
}
