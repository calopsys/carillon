# Carillon — Helm chart

Runs Carillon as a Kubernetes **CronJob**, with an optional bundled **Valkey**
(Redis-compatible) for persistent high-water marks. Installable straight from a
clone of this repo.

## Install

```sh
helm install carillon ./deploy/helm/carillon \
  --namespace devtools --create-namespace \
  --set secrets.files.github-token=ghp_xxxxxxxx \
  --set secrets.files.mattermost-webhook=https://mattermost.example.com/hooks/xxxx
```

Supply your own `config.toml` (the tracking config) instead of the built-in
default — `--set-file` reads the file into the `config` value verbatim:

```sh
helm install carillon ./deploy/helm/carillon \
  --set-file config=./config.toml \
  --set secrets.files.github-token=ghp_xxxxxxxx \
  --set secrets.files.mattermost-webhook=https://mattermost.example.com/hooks/xxxx
```

Render without installing (review / CI):

```sh
helm template carillon ./deploy/helm/carillon
```

## Secrets

`config.toml` never contains secret *values* — only references. On Kubernetes
the chart resolves them from **mounted files**, not env vars (env leaks into
`kubectl describe pod`, child processes, and crash dumps). The chart creates a
Secret and mounts it read-only at `secrets.mountPath` (default
`/var/run/secrets/carillon`); each `secrets.files` key becomes a file there, and
`config` reads it by path:

```yaml
secrets:
  files:
    github-token: ghp_xxxxxxxx
    mattermost-webhook: https://mattermost.example.com/hooks/xxxx
```

```toml
# matching config.toml references:
[credentials.github]
file = "/var/run/secrets/carillon/github-token"
[notify.webhooks]
mm_default = { file = "/var/run/secrets/carillon/mattermost-webhook" }
```

The keys are arbitrary filenames — use whatever your `config` references. Or
manage the Secret yourself and set `existingSecret` to its name (mounted the same
way) — handy with SealedSecrets / External Secrets.

> `CARILLON_REDIS_URL` stays an env var (the app reads it only from the
> environment). For the bundled Valkey it carries no password; if you point
> `externalRedisUrl` at a credentialed instance, treat that URL accordingly.

## State store

| Goal | Settings |
|------|----------|
| Bundled persistent Valkey (default) | `valkey.enabled=true` |
| Bring your own Redis/Valkey | `valkey.enabled=false`, `externalRedisUrl=redis://host:6379/0` |
| Stateless (re-notifies latest every run) | `valkey.enabled=false`, `externalRedisUrl=""` |

Valkey must keep its data: the chart sets `appendonly`+`noeviction` and mounts a
PVC. Carillon's keys (`carillon:hwm:<name>`) have no TTL; eviction or data loss
re-notifies each tracker's current latest.

## Key values

| Key | Default | Purpose |
|-----|---------|---------|
| `image.repository` | `ghcr.io/calopsys/carillon` | image |
| `image.tag` | `""` (⇒ `appVersion`) | image tag |
| `schedule` | `"0 * * * *"` | CronJob schedule (hourly) |
| `concurrencyPolicy` | `Forbid` | never overlap runs |
| `config` | inline default | the `config.toml` body |
| `secrets.create` | `true` | create the Secret from `secrets.files` |
| `secrets.files` | `{}` | name→value; each mounted as a file |
| `secrets.mountPath` | `/var/run/secrets/carillon` | where files are mounted |
| `existingSecret` | `""` | use a Secret you manage instead |
| `valkey.enabled` | `true` | bundle Valkey |
| `valkey.persistence.size` | `1Gi` | PVC size |
| `externalRedisUrl` | `""` | used when `valkey.enabled=false` |

See [`values.yaml`](values.yaml) for the full set (resources, security context,
scheduling).

## Ad-hoc run

```sh
kubectl create job --from=cronjob/carillon carillon-manual
```
