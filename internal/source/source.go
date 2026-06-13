// Package source fetches the available version tags for a tracked artifact from
// GitHub, GitLab, Forgejo (Gitea-family), or any OCI registry. All share one
// HTTP/auth/pagination approach via the helpers in this file.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/calopsys/carillon/internal/config"
)

// Tag is a single version tag with best-effort metadata for the notification.
type Tag struct {
	Name      string
	URL       string    // best-effort web link (may be empty)
	Body      string    // release notes (releases mode only)
	Published time.Time // publish time when known
}

// Source lists the tags currently available for one tracked artifact.
type Source interface {
	// ListTags returns all tags; filtering/ordering is the caller's job.
	ListTags(ctx context.Context) ([]Tag, error)
}

// New builds the right Source for a track. cred is the already-resolved
// credential value ("" means anonymous).
func New(t config.Track, cred string, hc *http.Client) (Source, error) {
	switch t.Source {
	case config.SourceGitHub:
		return newGitHub(t, cred, hc), nil
	case config.SourceGitLab:
		return newGitLab(t, cred, hc), nil
	case config.SourceForgejo:
		return newForgejo(t, cred, hc), nil
	case config.SourceOCI:
		return newOCI(t, cred, hc)
	default:
		return nil, fmt.Errorf("unknown source %q", t.Source)
	}
}

// DefaultClient returns the shared HTTP client used by all sources.
func DefaultClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// httpError carries the status code so retry logic can react to 5xx/429.
type httpError struct {
	status int
	body   string
	url    string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("GET %s: status %d: %s", e.url, e.status, truncate(e.body, 200))
}

// doJSON performs a GET with retry/backoff on transient failures and decodes a
// JSON body into out (if non-nil). It returns the response headers so callers
// can follow Link pagination. setHeaders may be nil.
func doJSON(ctx context.Context, hc *http.Client, url string, setHeaders func(*http.Request), out any) (http.Header, error) {
	const maxAttempts = 4
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if setHeaders != nil {
			setHeaders(req)
		}
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = err
		} else {
			hdr, err := decodeResp(resp, url, out)
			if err == nil {
				return hdr, nil
			}
			lastErr = err
			if !retryable(err) {
				// Return the header too: callers (OCI auth) need the
				// WWW-Authenticate challenge from a 401 response.
				return hdr, err
			}
		}
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

// maxBody bounds how much of a response we buffer. Release listings carry the
// full notes for up to 100 releases per page, which easily runs to several MB
// (e.g. argoproj/argo-cd), so this is generous; the +1 read below turns an
// overrun into a clear error instead of a silently truncated, undecodable body.
const maxBody = 64 << 20

func decodeResp(resp *http.Response, url string, out any) (http.Header, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		// A read failure means the body is truncated/corrupt; don't let it fall
		// through to the maxBody check or unmarshal and masquerade as success.
		return resp.Header, fmt.Errorf("GET %s: read body: %w", url, err)
	}
	if int64(len(body)) > maxBody {
		return resp.Header, fmt.Errorf("GET %s: response exceeds %d bytes", url, maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, &httpError{status: resp.StatusCode, body: string(body), url: url}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.Header, fmt.Errorf("decode %s: %w", url, err)
		}
	}
	return resp.Header, nil
}

func retryable(err error) bool {
	var he *httpError
	if asHTTPError(err, &he) {
		return he.status == http.StatusTooManyRequests || he.status >= 500
	}
	// Transport errors are retried directly in doJSON's request loop; the only
	// non-httpError reaching here is a permanent decode/oversize failure, which
	// re-fetching the identical response will never fix.
	return false
}

func asHTTPError(err error, target **httpError) bool {
	return errors.As(err, target)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// nextLink extracts the rel="next" URL from an RFC 5988 Link header, as used by
// both the GitHub and GitLab REST APIs (and OCI registries for tag pagination).
// It returns "" when there is no next page.
var linkNextRe = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func nextLink(h http.Header) string {
	for _, v := range h.Values("Link") {
		if m := linkNextRe.FindStringSubmatch(v); m != nil {
			return m[1]
		}
	}
	return ""
}

// resolveNext returns the absolute URL of the rel="next" page given the current
// request URL, or "" when there is no next page. A relative Link target (with or
// without a leading slash) is resolved against the current URL, as RFC 5988 and
// RFC 3986 permit — some registries emit one.
func resolveNext(current string, h http.Header) (string, error) {
	next := nextLink(h)
	if next == "" {
		return "", nil
	}
	base, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("parse current url %q: %w", current, err)
	}
	ref, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("parse next link %q: %w", next, err)
	}
	return base.ResolveReference(ref).String(), nil
}

// fetchPaged GETs startURL and follows rel="next" pagination, decoding each page
// into a fresh T and flattening mapPage(page) across all pages. It is the one
// pagination loop shared by the GitHub and GitLab sources.
func fetchPaged[T any](ctx context.Context, hc *http.Client, startURL string, setHeaders func(*http.Request), mapPage func(T) []Tag) ([]Tag, error) {
	var out []Tag
	for startURL != "" {
		var page T
		hdr, err := doJSON(ctx, hc, startURL, setHeaders, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, mapPage(page)...)
		startURL, err = resolveNext(startURL, hdr)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
