package cli

import (
	"fmt"

	"github.com/calopsys/carillon/internal/config"
	"github.com/calopsys/carillon/internal/version"

	"github.com/spf13/cobra"
)

func newValidateCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the config offline (regexes, compare groups, secret refs) and report the run mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*cfgPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			// Compile every effective pattern — catches bad regexes and compare
			// groups that don't exist in the pattern (beyond structural checks).
			for _, t := range cfg.Track {
				regex, compare, err := cfg.ResolvePattern(t)
				if err != nil {
					return err
				}
				if _, err := version.Compile(regex, compare); err != nil {
					return fmt.Errorf("track %q: %w", t.Name, err)
				}
			}

			// Verify every secret ref actually resolves (file readable / env set).
			if _, err := resolveCreds(cfg); err != nil {
				return err
			}
			if _, err := resolveWebhooks(cfg); err != nil {
				return err
			}

			fmt.Fprintf(out, "OK: %d tracker(s), %d pattern(s)\n", len(cfg.Track), len(cfg.Patterns))
			fmt.Fprintf(out, "state:  %s\n", stateMode())
			fmt.Fprintf(out, "notify: %s\n", notifyMode(cfg))
			return nil
		},
	}
}

func stateMode() string {
	if redisURL() == "" {
		return "stateless (no " + envRedisURL + "; every run is a first run)"
	}
	return "stateful (redis via " + envRedisURL + ")"
}

func notifyMode(cfg *config.Config) string {
	if !cfg.NotifyEnabled() {
		return "log-only (no [notify] configured)"
	}
	if cfg.Notify.DefaultWebhook != "" {
		return "mattermost (default webhook " + cfg.Notify.DefaultWebhook + ")"
	}
	return "mattermost (per-track webhooks)"
}
