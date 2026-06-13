# Quickstart — one-off run with Docker

Try Carillon against a handful of tools in about a minute, with no state store,
no secrets, and nothing to clean up. This is the throwaway path; for a scheduled
deployment with persistent state see **[deploy/](../deploy/)** (a Helm chart for
Kubernetes, or a Podman Quadlet + systemd timer for a single host).

## 1. Write a tiny config

Create a `config.toml` in the current directory. No `[notify]` and no state store
means Carillon runs **log-only** and **stateless** — it just prints the newest matching
version of each tracker and exits. The `semver` pattern is built in.

```toml
[[track]]
name    = "helm"
source  = "github"
repo    = "helm/helm"
pattern = "semver"

[[track]]
name    = "forgejo"
source  = "forgejo"        # host defaults to codeberg.org
repo    = "forgejo/forgejo"
ref     = "releases"
pattern = "semver"
```

See **[regex.md](regex.md)** for the pattern/compare model and
**[config.example.toml](config.example.toml)** for every option.

## 2. Run it

Mount the config at the path Carillon looks for by default
(`/etc/carillon/config.toml`):

```sh
# Check the config offline first (regexes, compare groups, secret refs):
docker run --rm \
  -v "$PWD/config.toml:/etc/carillon/config.toml:ro" \
  ghcr.io/calopsys/carillon:latest validate

# Then process every tracker once. In log-only mode the "notification" for each
# newest version is written to stderr as a JSON log line.
docker run --rm \
  -v "$PWD/config.toml:/etc/carillon/config.toml:ro" \
  ghcr.io/calopsys/carillon:latest run
```

Because there's no state store, **every run is a first run**: it always reports the
current latest. That's exactly what you want for a one-off look. To inspect a
single tracker (handy when tuning a regex), use `check` with `--dry-run`:

```sh
docker run --rm \
  -v "$PWD/config.toml:/etc/carillon/config.toml:ro" \
  ghcr.io/calopsys/carillon:latest check forgejo --dry-run
```
