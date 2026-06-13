package cli

import (
	"context"
	"fmt"

	"github.com/calopsys/carillon/internal/config"
	"github.com/calopsys/carillon/internal/run"
	"github.com/calopsys/carillon/internal/source"

	"github.com/spf13/cobra"
)

// buildVersion is overridable via -ldflags "-X .../internal/cli.buildVersion=v1.2.3".
var buildVersion = "dev"

// ExecuteContext builds and runs the root command with the given context (so
// SIGTERM/SIGINT cancels in-flight work).
func ExecuteContext(ctx context.Context) error {
	return newRoot().ExecuteContext(ctx)
}

func newRoot() *cobra.Command {
	var cfgPath string
	root := &cobra.Command{
		Use:           "carillon",
		Short:         "Track new releases of GitHub/GitLab tools and OCI images, notify Mattermost",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "path to config TOML (default $CARILLON_CONFIG or "+defaultConfigPath+")")

	root.AddCommand(
		newRunCmd(&cfgPath),
		newCheckCmd(&cfgPath),
		newValidateCmd(&cfgPath),
		newVersionCmd(),
	)
	return root
}

func loadConfig(cfgPath string) (*config.Config, error) {
	return config.Load(configPath(cfgPath))
}

func newRunCmd(cfgPath *string) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Process all trackers once (the CronJob entrypoint)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*cfgPath)
			if err != nil {
				return err
			}
			return doRun(cmd.Context(), cfg, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "fetch and compute, but never send notifications or write state")
	return cmd
}

func newCheckCmd(cfgPath *string) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "check <name>",
		Short: "Process a single tracker by name (handy for debugging regexes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgPath)
			if err != nil {
				return err
			}
			one, err := selectTrack(cfg, args[0])
			if err != nil {
				return err
			}
			return doRun(cmd.Context(), one, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "fetch and compute, but never send notifications or write state")
	return cmd
}

// selectTrack returns a shallow copy of cfg narrowed to a single track.
func selectTrack(cfg *config.Config, name string) (*config.Config, error) {
	for _, t := range cfg.Track {
		if t.Name == name {
			c := *cfg
			c.Track = []config.Track{t}
			return &c, nil
		}
	}
	return nil, fmt.Errorf("no tracker named %q in config", name)
}

func doRun(ctx context.Context, cfg *config.Config, dryRun bool) error {
	log := newLogger()

	creds, err := resolveCreds(cfg)
	if err != nil {
		return err
	}
	st, err := buildStore(ctx, dryRun, log)
	if err != nil {
		return err
	}
	defer st.Close()
	nf, err := buildNotifier(cfg, dryRun, log)
	if err != nil {
		return err
	}

	res := run.Run(ctx, cfg, run.Deps{
		Store:     st,
		Notifier:  nf,
		Creds:     creds,
		HTTP:      source.DefaultClient(),
		Logger:    log,
		NewSource: source.New,
	})
	if res.Errors > 0 {
		return fmt.Errorf("%d tracker(s) failed", res.Errors)
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Carillon version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "carillon "+buildVersion)
		},
	}
}
