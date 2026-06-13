// Package config loads and validates Carillon's TOML configuration (delivered
// as a Kubernetes ConfigMap) merged with environment variables.
package config

import (
	"fmt"
	"maps"
	"strings"

	"github.com/calopsys/carillon/internal/version"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Source kinds.
const (
	SourceGitHub  = "github"
	SourceGitLab  = "gitlab"
	SourceForgejo = "forgejo"
	SourceOCI     = "oci"
)

// Ref kinds for GitHub/GitLab.
const (
	RefTags     = "tags"
	RefReleases = "releases"
)

// Config is the whole parsed configuration.
type Config struct {
	// Concurrency bounds the per-tracker worker pool. Defaults to 4.
	Concurrency int `koanf:"concurrency"`
	// Patterns is a reusable library of named match/compare specs.
	Patterns map[string]Pattern `koanf:"patterns"`
	// Credentials maps a logical name to a secret ref (token / user:pass).
	Credentials map[string]Ref `koanf:"credentials"`
	// Notify is optional: absent => log-only mode.
	Notify *Notify `koanf:"notify"`
	// Track is the list of artifacts to watch.
	Track []Track `koanf:"track"`
}

// Pattern is a reusable comparison spec referenced by name from a Track.
type Pattern struct {
	Regex   string   `koanf:"regex"`
	Compare []string `koanf:"compare"`
}

// Notify configures Mattermost delivery.
type Notify struct {
	// DefaultWebhook names an entry in Webhooks used when a Track sets none.
	DefaultWebhook string `koanf:"default_webhook"`
	// Webhooks maps a logical name to a secret ref holding the webhook URL.
	Webhooks map[string]Ref `koanf:"webhooks"`
	// Template optionally overrides the message text/template.
	Template string `koanf:"template"`
}

// Track is a single artifact to watch.
type Track struct {
	Name   string `koanf:"name"`
	Source string `koanf:"source"` // github | gitlab | oci

	Repo    string `koanf:"repo"`     // github/gitlab "owner/name" or group path
	Image   string `koanf:"image"`    // oci, e.g. quay.io/minio/minio
	BaseURL string `koanf:"base_url"` // self-hosted GitLab / GitHub Enterprise / Forgejo

	Ref string `koanf:"ref"` // tags (default) | releases — github/gitlab/forgejo only

	// Pattern references a named [patterns.*]. Regex/Compare inline-override it.
	Pattern string   `koanf:"pattern"`
	Regex   string   `koanf:"regex"`
	Compare []string `koanf:"compare"`

	Credential string `koanf:"credential"`
	Webhook    string `koanf:"webhook"` // routing override; "" => default
}

// builtinPatterns are always available unless overridden by config.
func builtinPatterns() map[string]Pattern {
	return map[string]Pattern{
		"semver": {
			Regex:   `^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$`,
			Compare: []string{"major", "minor", "patch"},
		},
	}
}

// Load reads and parses the TOML file at path, applies defaults and builtin
// patterns, then validates. Environment overrides are applied by the caller
// via env-resolved secrets; structural config lives entirely in the file.
func Load(path string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}
	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Concurrency <= 0 {
		c.Concurrency = 4
	}
	merged := builtinPatterns()
	maps.Copy(merged, c.Patterns) // user entries override builtins
	c.Patterns = merged
}

// ResolvePattern returns the effective (regex, compare) for a track: an inline
// regex wins, otherwise the named pattern is looked up.
func (c *Config) ResolvePattern(t Track) (regex string, compare []string, err error) {
	if t.Regex != "" {
		return t.Regex, t.Compare, nil
	}
	if t.Pattern == "" {
		return "", nil, fmt.Errorf("track %q: no regex and no pattern reference", t.Name)
	}
	p, ok := c.Patterns[t.Pattern]
	if !ok {
		return "", nil, fmt.Errorf("track %q: unknown pattern %q", t.Name, t.Pattern)
	}
	return p.Regex, p.Compare, nil
}

// WebhookName returns the webhook name a track delivers to ("" if notify off).
func (c *Config) WebhookName(t Track) string {
	if c.Notify == nil {
		return ""
	}
	if t.Webhook != "" {
		return t.Webhook
	}
	return c.Notify.DefaultWebhook
}

// NotifyEnabled reports whether config requests Mattermost delivery. A
// default_webhook is not required: tracks may each route via an explicit
// per-track webhook, so the presence of any webhook is enough.
func (c *Config) NotifyEnabled() bool {
	return c.Notify != nil && len(c.Notify.Webhooks) > 0
}

// Validate checks structural integrity without any network or secret access
// beyond confirming refs are well-formed.
func (c *Config) Validate() error {
	if len(c.Track) == 0 {
		return fmt.Errorf("no [[track]] entries configured")
	}
	for name, p := range c.Patterns {
		if p.Regex == "" {
			return fmt.Errorf("pattern %q: empty regex", name)
		}
	}
	if c.Notify != nil {
		if c.Notify.DefaultWebhook != "" {
			if _, ok := c.Notify.Webhooks[c.Notify.DefaultWebhook]; !ok {
				return fmt.Errorf("notify: default_webhook %q not found in [notify.webhooks]", c.Notify.DefaultWebhook)
			}
		}
		for name, ref := range c.Notify.Webhooks {
			if err := ref.Validate(); err != nil {
				return fmt.Errorf("notify.webhooks.%s: %w", name, err)
			}
		}
	}
	for name, ref := range c.Credentials {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("credentials.%s: %w", name, err)
		}
	}
	seen := make(map[string]bool, len(c.Track))
	for i, t := range c.Track {
		where := fmt.Sprintf("track[%d]", i)
		if t.Name == "" {
			return fmt.Errorf("%s: missing name", where)
		}
		where = fmt.Sprintf("track %q", t.Name)
		if seen[t.Name] {
			return fmt.Errorf("%s: duplicate name", where)
		}
		seen[t.Name] = true

		switch t.Source {
		case SourceGitHub, SourceGitLab, SourceForgejo:
			if t.Repo == "" {
				return fmt.Errorf("%s: source %q requires repo", where, t.Source)
			}
			if t.Image != "" {
				return fmt.Errorf("%s: image is only valid for source oci", where)
			}
			if t.Ref != "" && t.Ref != RefTags && t.Ref != RefReleases {
				return fmt.Errorf("%s: ref must be %q or %q", where, RefTags, RefReleases)
			}
		case SourceOCI:
			if t.Image == "" {
				return fmt.Errorf("%s: source oci requires image", where)
			}
			if t.Repo != "" {
				return fmt.Errorf("%s: repo is only valid for source github/gitlab/forgejo", where)
			}
			if t.Ref != "" {
				return fmt.Errorf("%s: ref is only valid for source github/gitlab/forgejo", where)
			}
		case "":
			return fmt.Errorf("%s: missing source", where)
		default:
			return fmt.Errorf("%s: unknown source %q (want github|gitlab|forgejo|oci)", where, t.Source)
		}

		regex, compare, err := c.ResolvePattern(t)
		if err != nil {
			return err
		}
		// Compile here so a malformed regex or a compare group missing from the
		// pattern fails preflight validation instead of at runtime.
		if _, err := version.Compile(regex, compare); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if t.Credential != "" {
			if _, ok := c.Credentials[t.Credential]; !ok {
				return fmt.Errorf("%s: unknown credential %q", where, t.Credential)
			}
		}
		if c.Notify == nil {
			if t.Webhook != "" {
				return fmt.Errorf("%s: webhook %q set but no [notify] section", where, t.Webhook)
			}
		} else {
			// With [notify] present every track must resolve to a real webhook —
			// its own override or the default. Catch a missing/empty resolution
			// here rather than at first-delivery time.
			name := c.WebhookName(t)
			if name == "" {
				return fmt.Errorf("%s: no webhook to deliver to (set this track's webhook or notify.default_webhook)", where)
			}
			if _, ok := c.Notify.Webhooks[name]; !ok {
				return fmt.Errorf("%s: unknown webhook %q", where, name)
			}
		}
	}
	return nil
}

// Identifier returns the source identifier used for logging/audit (repo or image).
func (t Track) Identifier() string {
	if t.Source == SourceOCI {
		return t.Image
	}
	return t.Repo
}

// EffectiveRef returns the github/gitlab ref kind, defaulting to tags.
func (t Track) EffectiveRef() string {
	if t.Ref == "" {
		return RefTags
	}
	return strings.ToLower(t.Ref)
}
