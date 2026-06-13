package config

import (
	"fmt"
	"os"
	"strings"
)

// Ref points at a secret value resolved from either a mounted file or an
// environment variable — never the (ConfigMap) TOML itself. This is the single
// resolution path used everywhere a secret is needed (API tokens, the Redis
// URL, Mattermost webhook URLs). Exactly one of File or Env must be set.
type Ref struct {
	File string `koanf:"file"`
	Env  string `koanf:"env"`
}

// Zero reports whether the ref points at nothing.
func (r Ref) Zero() bool { return r.File == "" && r.Env == "" }

// Validate checks that exactly one source is configured.
func (r Ref) Validate() error {
	switch {
	case r.File == "" && r.Env == "":
		return fmt.Errorf("secret ref has neither file nor env")
	case r.File != "" && r.Env != "":
		return fmt.Errorf("secret ref sets both file and env (pick one)")
	}
	return nil
}

// Resolve reads the secret value, trimming a single trailing newline (common
// when secrets are written with an editor or `kubectl create secret`).
func (r Ref) Resolve() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	if r.Env != "" {
		v, ok := os.LookupEnv(r.Env)
		if !ok {
			return "", fmt.Errorf("env %q is not set", r.Env)
		}
		return strings.TrimRight(v, "\r\n"), nil
	}
	b, err := os.ReadFile(r.File)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", r.File, err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}
