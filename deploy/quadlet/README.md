# Carillon — Podman Quadlet + systemd timer

Run Carillon on a host with Podman: a long-running **Valkey** container holds the
high-water marks, and a **systemd timer** fires Carillon as a one-shot run on a
schedule. No Docker, no Compose — Quadlet turns the unit files below into systemd
services, and `systemd` does the scheduling.

Requires Podman 4.4+ (Quadlet) — included in recent Fedora/RHEL/Debian/Ubuntu.

## Files

| File | Role |
|------|------|
| `carillon.network` | shared network (so the run container resolves `valkey` by name) |
| `valkey.volume` | named volume for `/data` (the marks) |
| `valkey.container` | the always-on Valkey service → `valkey.service` |
| `carillon.container` | the one-shot run (`Type=oneshot`) → `carillon.service` |
| `carillon.timer` | the schedule; activates `carillon.service` |
| `valkey.conf` | Valkey persistence + `noeviction` |
| `config.toml` | the tracking config (env-ref secrets only) |
| `carillon.env.example` | secrets template → copy to `carillon.env` |

## Install (rootless — recommended)

```sh
# 1. Unit files -> the Quadlet search path.
mkdir -p ~/.config/containers/systemd
cp carillon.network valkey.volume valkey.container carillon.container carillon.timer \
   ~/.config/containers/systemd/

# 2. Data + secrets -> referenced by %h/.config/carillon in the units.
mkdir -p ~/.config/carillon
cp valkey.conf config.toml ~/.config/carillon/
cp carillon.env.example ~/.config/carillon/carillon.env
chmod 600 ~/.config/carillon/carillon.env
$EDITOR ~/.config/carillon/carillon.env       # fill in token + webhook
$EDITOR ~/.config/carillon/config.toml         # adjust what you track

# 3. Generate the services and start things.
systemctl --user daemon-reload
systemctl --user enable --now valkey.service   # bring Valkey up (and keep it up)
systemctl --user enable --now carillon.timer   # arm the schedule

# 4. Let user services run without an active login session (servers!).
loginctl enable-linger "$USER"
```

Do **not** `enable carillon.service` — the timer drives it. Enabling it directly
would also start a run at boot.

## Operate

```sh
systemctl --user start carillon.service              # run once, now
systemctl --user list-timers carillon.timer          # when does it next fire?
journalctl --user -u carillon.service -f             # logs of the runs
journalctl --user -u valkey.service                  # state-store logs
```

Change the cadence by editing `[Timer] OnCalendar=` in `carillon.timer`
(e.g. `*-*-* *:00,30:00` for every 30 min), then `systemctl --user daemon-reload`.

## Rootful variant

Run as root for a system-wide service instead:

- Put the unit files in `/etc/containers/systemd/`.
- Put `valkey.conf`, `config.toml`, `carillon.env` in `/etc/carillon/` and change
  the `%h/.config/carillon` paths in `valkey.container` / `carillon.container` to
  `/etc/carillon`.
- Drop the `--user` flag from every `systemctl` / `journalctl` command. Linger is
  not needed.

## State store

Valkey must stay persistent and `noeviction` (see `valkey.conf`): Carillon's keys
have no TTL, and an evicted/lost key re-notifies that tracker's current latest on
the next run. The marks live in the `valkey.volume` named volume — inspect them
with `podman exec valkey valkey-cli --scan --pattern 'carillon:hwm:*'`.
