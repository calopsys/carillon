# AGENTS.md

Project rules and conventions for agents working on Carillon. Keep this current.

## What Carillon is

A Go CLI that tracks new releases of GitHub/GitLab/Forgejo tools and OCI images
and posts to a Mattermost webhook. Runs to completion once per invocation (Kubernetes
CronJob). CLI-only; no web UI. See `README.md` and `docs/`.

## Stack

- Go (module `github.com/calopsys/carillon`).
- **cobra** (CLI), **go-redis/v9** (Redis/Valkey state store), **koanf** (TOML + env),
  stdlib **slog** (JSON logs), raw **net/http** for every source and the webhook.
- No source SDKs (no go-github/gitlab/gitea client). One shared HTTP/auth/
  pagination path in `internal/source` is intentional — keep it that way.
- State is a pure key→value high-water mark (one key per tracker), so the store
  is a Redis-compatible KV (Redis or Valkey), not a SQL database — no schema, no
  migrations.

## Layout

```plain
cmd/carillon/        # main: signal context -> cli.ExecuteContext
internal/cli/        # cobra tree + dependency wiring + mode selection
internal/config/     # koanf load + validate + pattern resolution; Ref (file/env secret resolver)
internal/source/     # Source.ListTags; github/gitlab/forgejo REST + oci registry v2
internal/version/    # pattern compile, candidate filter, integer compare key
internal/store/      # Store: Redis/Valkey (go-redis) + NoOp; key carillon:hwm:<name>
internal/notify/     # Notifier: mattermost (templated) + log-only
internal/run/        # orchestrator: pool, per-tracker pipeline
docs/                # quickstart + config example + regex guide
deploy/              # helm chart (k8s CronJob) + podman quadlet (systemd timer); both with Valkey
```

## Build & test

```sh
go build ./...
go test ./...
go vet ./...
go build -o /tmp/carillon ./cmd/carillon && /tmp/carillon validate -c docs/config.example.toml
```

Tests must not rely on loopback TCP (this dev sandbox blocks it). Test HTTP
clients with an in-memory `http.RoundTripper` (see `internal/source/oci_test.go`),
not `httptest.NewServer`.

## Design invariants (do not break without discussion)

- **One comparison mechanism**: RE2 `pattern` (full-match = candidate) + ordered
  integer `compare` groups; an absent optional numeric group counts as 0 (so
  variable-length versions compare correctly). No semver library, no pre-release/
  build semantics, no string comparison. The regex is also the tracking scope.
- **Send-then-persist**: notify first, advance the high-water mark only if
  delivery succeeded (at-least-once; an outage retries next run).
- **Catch-up = newest only**: one notification for the max, never per-missed.
- **First sighting notifies** the current latest (one message), then records it.
- **State is keyed by `name`** (stable id). Renaming re-baselines. The stored
  mark carries a fingerprint of its comparison basis (regex + `compare` groups);
  any pattern change shifts the fingerprint and re-baselines, rather than
  comparing keys built on different bases.
- **Per-tracker isolation**: one failure never sinks the run; non-zero exit if
  any tracker errored.
- **Secrets never in the ConfigMap**: resolve via `config.Ref` (file/env).
  Never read the k8s API.
- **Notify and state are independently optional**: no `[notify]` ⇒ log-only; no
  `CARILLON_REDIS_URL` ⇒ stateless. `--dry-run` forces both off for one invocation.
- **Redis/Valkey must be persistent + `noeviction`**: keys (`carillon:hwm:<name>`)
  have no TTL; eviction/loss re-baselines a tracker and re-notifies its current latest.

## Conventions

- Structured logging via slog; user-facing command output via `cmd.OutOrStdout()`.
- New source kinds implement `source.Source` and are dispatched in `source.New`.
- Keep `docs/config.example.toml` and `docs/*` in sync with config changes.
