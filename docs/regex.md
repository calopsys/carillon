# Version matching & comparison

Carillon uses **one** mechanism to decide which tags to track and which is
newest: an [RE2](https://github.com/google/re2/wiki/Syntax) regular expression
plus an ordered list of capture groups to compare as integers.

## The two fields

```toml
regex   = '^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$'
compare = ["major", "minor", "patch"]
```

- **`regex`** — a tag must **fully match** it (anchored end-to-end) to be a
  *candidate*. Anything that doesn't match is ignored entirely: git hashes,
  `latest`, `nightly`, release-candidate suffixes, etc. This means the regex
  **doubles as the tracking scope** — see below.
- **`compare`** — the named capture groups to build the comparison key from,
  **most significant first**. Each is compared **as an integer**. The newest tag
  is the one with the greatest key tuple.

### `compare` is optional

If you omit `compare`, it defaults to **all named groups, in left-to-right
order**. So these are equivalent:

```toml
regex = '^v(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$'
# compare = ["major", "minor", "patch"]   # implied
```

Groups that are captured but **not** listed in `compare` are *match-only* (used
to filter, ignored for ordering). If you have a non-numeric match-only group,
list the numeric ones in `compare` explicitly so the non-numeric one is excluded.

## Reusable named patterns

Define a pattern once under `[patterns.<name>]` and reference it by name:

```toml
[patterns.minio_release]
regex   = '^RELEASE\.(?P<year>\d+)-(?P<month>\d+)-(?P<day>\d+)T.*Z$'
compare = ["year", "month", "day"]

[[track]]
name    = "minio"
source  = "oci"
image   = "quay.io/minio/minio"
pattern = "minio_release"
```

`semver` is **built in** (you can use `pattern = "semver"` without defining it);
redefining `[patterns.semver]` overrides the builtin. An inline `regex`/`compare`
on a `[[track]]` overrides any `pattern` reference.

## Why integer compare, not string

`v1.10.0` is **newer** than `v1.9.0`. Lexicographically `"10" < "9"`, which is
wrong; numerically `10 > 9`, which is right. Carillon always compares the groups
as integers, so double-digit segments order correctly.

> Carillon intentionally has **no** semver pre-release/build semantics. It only
> compares the integer groups you name. Don't track `-rc`/`-beta` tags — exclude
> them with the regex (a trailing `-rc1` simply won't fully match `…(?P<patch>\d+)$`).

## Variable-length versions (optional components)

When a project mixes shapes — `1.28`, `1.27.1`, `1.27.1-2` — make the trailing
components optional and list them all in `compare`. An **absent numeric component
counts as 0**, so every tag yields a constant-length key that orders correctly:

```toml
regex   = '^v?(?P<major>\d+)\.(?P<minor>\d+)(?:\.(?P<patch>\d+))?(?:-(?P<rev>\d+))?$'
compare = ["major", "minor", "patch", "rev"]
```

```
1.27.1   -> [1, 27, 1, 0]
1.27.1-2 -> [1, 27, 1, 2]   # rev ranks: newer than 1.27.1
1.28     -> [1, 28, 0, 0]   # minor dominates: newer than 1.27.1-2
```

Each optional group is still `\d+`, so a non-numeric tail (`1.27.x`) simply
doesn't match and is ignored. If a suffix like `-2` is downstream packaging you'd
rather *not* rank on, leave `rev` out of `compare` (capture it but don't compare
it) so only `major.minor.patch` decide order.

## The regex is your tracking scope

Because a tag must fully match to be considered, the regex decides *what you are
tracking*, not just how to parse it.

| Goal | Pattern |
| --- | --- |
| Any semver | `^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$` |
| Only the v1 line | `^v1\.(?P<minor>\d+)\.(?P<patch>\d+)$` |
| Only v1 and v2 | `^v(?P<major>[12])\.(?P<minor>\d+)\.(?P<patch>\d+)$` |
| Date-based (MinIO) | `^RELEASE\.(?P<year>\d+)-(?P<month>\d+)-(?P<day>\d+)T.*Z$` |

With the v1-line pattern, `v2.0.0` never matches, so Carillon will never notify
about it — you've pinned tracking to v1.

## Compare priority ≠ regex order

`compare` lists groups in *comparison* priority, which need not match their order
in the string. A `DD-MM-YYYY` tag captures day first but should compare year
first:

```toml
regex   = '^(?P<day>\d+)-(?P<month>\d+)-(?P<year>\d+)$'
compare = ["year", "month", "day"]
```

## Detection semantics (recap)

- Each run computes `latest = max(matching tags)`.
- **First sighting** of a tracker notifies with `latest` (one message), then
  records it. Adding N trackers at once produces N messages.
- If several versions shipped since the last run, you get **one** notification
  for the newest; intermediate versions are not announced.
- A notification is sent **before** the high-water mark is persisted, and the
  mark only advances if delivery succeeded — so an outage retries next run rather
  than silently dropping a release.

## Debugging a pattern

Use `check` with `--dry-run` to see what a tracker would do against live tags,
sending nothing and writing nothing:

```sh
carillon check minio --dry-run -c config.toml
```
