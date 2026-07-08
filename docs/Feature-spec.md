# `Feature` manifest spec

Kind: `Feature` — the desired-state file for a single Unleash feature
flag. One file per flag, under `flags/*.yaml`. See
`archive/unleash-cli-tech-spec.md` for the full CLI picture (contexts, `sync`,
ownership scoping); this doc covers just the `Feature` manifest itself
and the commands that read/write it.

## Schema

```yaml
apiVersion: unleashctl/v1
kind: Feature
metadata:
  name: new-checkout          # always in the `default` project
  service: payments           # becomes a real Unleash tag: service:payments
spec:                              # applies everywhere unless overridden
  type: release
  description: New checkout flow
  enabled: true
  impressionData: true
  strategies:
    - name: flexibleRollout
      parameters: { rollout: "25", stickiness: userId, groupId: new-checkout }
links:                             # optional — additional links for the feature
  - url: https://wiki.internal/new-checkout
    title: Design doc
tags:                              # optional — additional tags, alongside the automatic service tag
  - type: team
    value: checkout
envOverride:                       # optional — applies to every context on this environment
  development:
    strategies:
      - name: default              # 100%/default on dev *and* stage — both are `development`
contextOverride:                   # optional — applies to exactly one context, wins over envOverride
  prod:
    enabled: false                 # off in prod until the rollout above is ready
```

`metadata.service` is applied to the underlying feature as an Unleash
tag (type `service`, value `payments`) on create — a real, visible
artifact on the instance, not something only this CLI/repo knows about.
It's the single scoping mechanism for which features a given
`sync`/`diff`/`apply` invocation considers, and how conflicts between
repos are detected. Strongly recommended on every flag; a flag with no
`service` is legal but opts out of all of that scoping.

`links` and `tags` are declared once per feature and always apply
regardless of environment or context — unlike `spec`, they don't
participate in `envOverride`/`contextOverride` resolution, since neither
concept is environment-scoped in Unleash itself. `tags` is in addition
to the automatic `service` tag described above — both end up applied to
the feature.

### `envOverride` / `contextOverride`

Two optional override blocks sit on top of `spec`, for two different
kinds of exception:

- **`envOverride`**, keyed by **environment name** (`development`/
  `production`) — applies to *every* context configured with that
  environment.
- **`contextOverride`**, keyed by **context name** (`dev`/`stage`/
  `prod`/...) — applies to exactly one instance, and wins over
  `envOverride` where both touch the same field.

Resolution rule: for a given `(feature, context)` pair, start from
`spec`, shallow-merge any keys under `envOverride.<environment-name>`
(the context's configured environment), then shallow-merge any keys
under `contextOverride.<context-name>` on top of that. Arrays like
`strategies` are replaced wholesale at each step, not merged
element-wise. A feature with neither override block is, by
construction, identical everywhere.

## Commands

```
unleashctl diff  --context prod                              # environment implied by context config
unleashctl apply --context prod [--yes]                       # apply the diff
unleashctl diff  --context prod --service payments            # scope to one service only
unleashctl diff  --context prod --service payments --archive-missing  # + drift-correct: archive
                                                                        # remote flags in this service
                                                                        # scope with no local file
unleashctl apply --context prod --service payments --archive-missing -i  # same, but confirm
                                                                          # each candidate one by one
```

`--service` is optional; without it, the command covers every service
represented in `flags/`, diffing/applying each one independently.

`diff` computes, per service present in scope: fetch remote features
tagged with that service, resolve the matching local files (applying
`envOverride`/`contextOverride`), and diff the two directly. The result
is a reviewable set of **create/update** operations. A remote feature
tagged with this service that has no matching local file is surfaced as
informational only ("N flag(s) in this service have no local file — not
archiving; rerun with `--archive-missing` to review") unless
`--archive-missing` is passed.

**By default, `diff`/`apply` are additive-only: create and update, never
archive.** A file missing from `flags/` is never treated as "this flag
should be removed from the instance" unless `--archive-missing` is
passed explicitly — this matters because multiple MRs can be open
against the same `service` scope at once, and a prune-by-default `diff`
would misread a locally-absent file as an intended delete.

`--archive-missing` requires `--service`. Confirmation is batch by
default (`apply --archive-missing` prints the full candidate list and
asks for one confirmation, or `--yes` to skip in CI); `-i`/`--interactive`
goes through the list one flag at a time instead. `-i` and `--yes` are
mutually exclusive.

## Exit codes

`diff` (and `apply`) exit `0` whether or not there are pending changes —
drift alone is not a failure, so a diff-only CI job doesn't break just
because something changed. Exit `1` only on a real error: fetching
failed, or `apply`/`diff` refused due to an ownership conflict
(mismatched/untagged `service`, see `archive/unleash-cli-tech-spec.md` §6.4).

## Ownership scoping

Full detail in `archive/unleash-cli-tech-spec.md` §6.4. Summary: a local file's
`metadata.service` must match the remote feature's `service` tag (or the
remote feature must be untagged and adopted via `--adopt`) — otherwise
`apply` refuses rather than silently overwriting a flag owned by another
repo/service.
