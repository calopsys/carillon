// Package cli builds Carillon's cobra command tree and wires the concrete
// dependencies (config, store, notifier) selected by environment and flags.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/calopsys/carillon/internal/config"
	"github.com/calopsys/carillon/internal/notify"
	"github.com/calopsys/carillon/internal/source"
	"github.com/calopsys/carillon/internal/store"
)

// Environment variable names.
const (
	envConfig   = "CARILLON_CONFIG"
	envRedisURL = "CARILLON_REDIS_URL"
)

const defaultConfigPath = "/etc/carillon/config.toml"

func configPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envConfig); v != "" {
		return v
	}
	return defaultConfigPath
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// resolveCreds reads every configured credential secret up front.
func resolveCreds(cfg *config.Config) (map[string]string, error) {
	out := make(map[string]string, len(cfg.Credentials))
	for name, ref := range cfg.Credentials {
		v, err := ref.Resolve()
		if err != nil {
			return nil, fmt.Errorf("credential %q: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}

// resolveWebhooks reads every configured webhook URL.
func resolveWebhooks(cfg *config.Config) (map[string]string, error) {
	out := map[string]string{}
	if cfg.Notify == nil {
		return out, nil
	}
	for name, ref := range cfg.Notify.Webhooks {
		v, err := ref.Resolve()
		if err != nil {
			return nil, fmt.Errorf("webhook %q: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}

// redisURL returns the Redis URL from the environment ("" if stateless).
func redisURL() string { return os.Getenv(envRedisURL) }

// buildStore selects the Store. dryRun or an absent Redis URL yields a NoOp
// (stateless) store; otherwise it connects to Redis.
func buildStore(ctx context.Context, dryRun bool, log *slog.Logger) (store.Store, error) {
	url := redisURL()
	if dryRun || url == "" {
		if dryRun {
			log.Info("stateless: --dry-run (no state read or written)")
		} else {
			log.Info("stateless: no " + envRedisURL + " set (every run is a first run)")
		}
		return store.NoOp{}, nil
	}
	rdb, err := store.OpenRedis(ctx, url)
	if err != nil {
		return nil, err
	}
	log.Info("stateful: redis")
	return rdb, nil
}

// buildNotifier selects the Notifier. dryRun or notify-not-configured yields the
// log-only notifier; otherwise Mattermost with resolved webhook URLs.
func buildNotifier(cfg *config.Config, dryRun bool, log *slog.Logger) (notify.Notifier, error) {
	if dryRun || !cfg.NotifyEnabled() {
		if dryRun {
			log.Info("log-only: --dry-run (no webhook calls)")
		} else {
			log.Info("log-only: no [notify] configured")
		}
		return notify.LogNotifier{Logger: log}, nil
	}
	hooks, err := resolveWebhooks(cfg)
	if err != nil {
		return nil, err
	}
	mm, err := notify.NewMattermost(hooks, cfg.Notify.Template, source.DefaultClient())
	if err != nil {
		return nil, err
	}
	log.Info("notify: mattermost", "default_webhook", cfg.Notify.DefaultWebhook)
	return mm, nil
}
