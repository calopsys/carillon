package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndBuiltinSemver(t *testing.T) {
	p := writeTmp(t, `
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Concurrency != 4 {
		t.Fatalf("default concurrency want 4, got %d", c.Concurrency)
	}
	if _, ok := c.Patterns["semver"]; !ok {
		t.Fatal("builtin semver pattern missing")
	}
	rx, cmp, err := c.ResolvePattern(c.Track[0])
	if err != nil || rx == "" || len(cmp) != 3 {
		t.Fatalf("resolve semver: rx=%q cmp=%v err=%v", rx, cmp, err)
	}
	if c.Track[0].EffectiveRef() != RefTags {
		t.Fatalf("default ref should be tags")
	}
}

func TestUserPatternOverridesBuiltin(t *testing.T) {
	p := writeTmp(t, `
[patterns.semver]
regex = '^(?P<major>\d+)$'
compare = ["major"]

[[track]]
name = "a"
source = "oci"
image = "nginx"
pattern = "semver"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Patterns["semver"].Regex != `^(?P<major>\d+)$` {
		t.Fatalf("user semver override not applied: %q", c.Patterns["semver"].Regex)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"duplicate name": `
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"
[[track]]
name = "a"
source = "github"
repo = "o/r2"
pattern = "semver"`,
		"unknown source": `
[[track]]
name = "a"
source = "svn"
repo = "o/r"
pattern = "semver"`,
		"oci without image": `
[[track]]
name = "a"
source = "oci"
pattern = "semver"`,
		"github without repo": `
[[track]]
name = "a"
source = "github"
pattern = "semver"`,
		"unknown pattern": `
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "nope"`,
		"unknown credential": `
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"
credential = "ghost"`,
		"ref on oci": `
[[track]]
name = "a"
source = "oci"
image = "nginx"
ref = "releases"
pattern = "semver"`,
		"track with no resolvable webhook": `
[notify]
[notify.webhooks]
team = { env = "X" }
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTmp(t, body)); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}

func TestNotifyEnabledWithoutDefaultWebhook(t *testing.T) {
	// [notify] with only per-track webhooks (no default_webhook) is valid and
	// must still count as notify-enabled — not silently downgraded to log-only.
	p := writeTmp(t, `
[notify]
[notify.webhooks]
team = { env = "X" }

[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"
webhook = "team"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("per-track webhook with no default should be valid: %v", err)
	}
	if !c.NotifyEnabled() {
		t.Fatal("NotifyEnabled() should be true when webhooks exist without a default")
	}
}

func TestWebhookOverrideRequiresNotify(t *testing.T) {
	p := writeTmp(t, `
[[track]]
name = "a"
source = "github"
repo = "o/r"
pattern = "semver"
webhook = "x"
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error: webhook override with no [notify]")
	}
}
