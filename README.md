# Carillon

Carillon watches **GitHub** tools, **GitLab** tools, **Forgejo** (Codeberg)
tools, and **OCI images** (Docker Hub, GHCR, Quay, …) for new releases and posts
to a **Mattermost** webhook when
one appears. It's a single Go binary designed to run as a Kubernetes CronJob: it
processes every tracked artifact once and exits.

```sh
carillon run                       # the CronJob entrypoint
carillon check traefik --dry-run   # debug one tracker, send/persist nothing
carillon validate                  # check config offline, report run mode
```

## How it works

- You declare what to track in a **TOML config** (a Kubernetes ConfigMap).
- Each tracker uses a **regex + integer compare key** to pick matching tags and
  find the newest — the regex also scopes *what* you track (e.g. only the v1
  line). See **[docs/regex.md](docs/regex.md)**.
- The last-notified version per tracker is stored in **Redis/Valkey** (or nowhere,
  in stateless mode). Notification is sent *before* the mark advances, so an outage
  retries rather than silently dropping a release.
- Secrets (tokens, Redis URL, webhook URLs) are resolved from **mounted files or
  env vars**, never from the ConfigMap.

## Docs

- **[docs/quickstart.md](docs/quickstart.md)** — try it in a minute with one `docker run`, no state store or secrets.
- **[docs/config.example.toml](docs/config.example.toml)** — a complete, commented config.
- **[docs/regex.md](docs/regex.md)** — the pattern/compare model and worked examples.
- **[deploy/helm/carillon/](deploy/helm/carillon/)** — Helm chart: Carillon as a Kubernetes CronJob with a bundled persistent Valkey.
- **[deploy/quadlet/](deploy/quadlet/)** — Podman Quadlet + systemd timer: a Valkey container and a scheduled one-shot run on a single host.

## Build

```sh
go build -o carillon ./cmd/carillon
go test ./...
```
