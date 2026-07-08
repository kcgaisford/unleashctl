# unleashctl

unleashctl is a CLI that manages feature flags as declarative manifests —
the same way `kubectl` manages Kubernetes manifests. `flags/*.yaml` is your
desired state, checked into git; `unleashctl diff`/`apply` reconciles a live
Unleash instance against it, the same way `kubectl diff`/`apply` reconciles
a cluster against your YAML. A "migration" is just a git-tracked change to
those same files — there's no separate migration format to author.

Built in Go on Cobra + Viper, wrapping the Unleash Admin API. Manifest specs:
[`docs/Feature-spec.md`](./docs/Feature-spec.md),
[`docs/ContextField-spec.md`](./docs/ContextField-spec.md), and
[`docs/Segment-spec.md`](./docs/Segment-spec.md). 

## Prerequisites

- Go 1.26+
- A running Unleash instance (OSS) — [Docker quick start](https://docs.getunleash.io/quickstart)
- An Admin API token (personal access token or service account token) for that instance

## Build

```
git clone git@github.com:kcgaisford/unleashctl.git
cd unleashctl
go build -o unleashctl .
```

or `make build`.

## Configure a context

unleashctl reads contexts from `./.unleashctl.yaml` (project-local, checked
first) or `~/.unleashctl/config.yaml` (global). A context pairs an instance
URL with the one environment that instance has enabled, and names an env var
to read the token from — **never put the token itself in this file**:

```yaml
current-context: dev
contexts:
  - name: dev
    url: https://dev-unleash.internal
    environment: development
    token-env: UNLEASH_DEV_TOKEN
  - name: prod
    url: https://prod-unleash.internal
    environment: production
    token-env: UNLEASH_PROD_TOKEN
```

Then export the token before running any command:

```
export UNLEASH_DEV_TOKEN=<your admin token>
```

Every setting can also be passed without a config file at all — useful for
CI — using `--url`/`--token`/`--environment`, or the `UNLEASHCTL_URL` /
`UNLEASHCTL_TOKEN` env vars. Precedence is flag > env > context.

### `ui_managed_enabled`: let the UI own on/off for a context

Set `ui_managed_enabled: true` on a context (typically prod) when an
engineer is expected to flip a flag on by hand in the Unleash UI, rather
than by editing `flags/*.yaml`. For that context, `diff`/`apply` never
compare or push the `enabled` field — the live value is authoritative and
is left alone. Everything else (strategies, rollout parameters, type,
description, tags) keeps reconciling from `flags/*.yaml` exactly as usual.
A brand-new feature still lands disabled, since Unleash itself defaults a
newly created environment to `enabled: false`. Both `diff` and `apply`
print a one-line note when this is active, so it's clear why `enabled`
never shows up as a pending change.

```yaml
  - name: prod
    url: https://prod-unleash.internal
    environment: production
    token-env: UNLEASH_PROD_TOKEN
    ui_managed_enabled: true
```

## Author flags/*.yaml

[`docs/Feature-spec.md`](./docs/Feature-spec.md)

Each feature is one file under `flags/`, with a `metadata.service` tag that
becomes a real `service:<name>` tag on the Unleash feature — this is what
scopes `diff`/`apply` when multiple repos share one OSS instance.

```yaml
apiVersion: unleashctl/v1
kind: Feature
metadata:
  name: new-checkout
  service: payments
spec:
  type: release
  description: New checkout flow
  enabled: true
  impressionData: true
  strategies:
    - name: flexibleRollout
      parameters: { rollout: "25", stickiness: userId, groupId: new-checkout }
links:
  - url: https://wiki.internal/new-checkout
    title: Design doc
tags:
  - type: team
    value: checkout
envOverride:
  development:
    strategies:
      - name: default
contextOverride:
  prod:
    enabled: false
```

`envOverride` (keyed by environment name) and `contextOverride` (keyed by
context name, wins on conflict) are optional — a file with neither is
identical on every instance. See `docs/Feature-spec.md` for the full
resolution rule.

`links` and `tags` are optional, top-level (siblings of `metadata`/`spec`),
and always apply regardless of environment/context — they don't go through
`envOverride`/`contextOverride`. `tags` is in addition to the automatic
`service` tag above.

## Usage

This repo bundles example manifests under `examples/` (`examples/flags`,
`examples/contexts`, `examples/segments`) — pass `--flags-dir examples/flags`
(and `--contexts-dir`/`--segments-dir` for those kinds) to try the commands
below against them. In your own project, these directories live at the repo
root and the defaults (`flags`, `contexts`, `segments`) just work.

```
unleashctl diff --context dev                       # what would change
unleashctl apply --context dev --yes                # apply it
unleashctl diff  --context dev --service payments    # scope to one service
unleashctl apply --context dev --dry-run             # print the request payload only
```

Exit codes: `0` whether or not changes are pending (drift alone never
fails a diff-only CI job), `1` only on a real error.

`diff`/`apply` are additive-only by default: a remote feature tagged with a
service but missing a local file is reported informationally, never treated
as a delete.

### `--archive-missing`: explicit cleanup

Pass `--archive-missing` (requires `--service`) to turn that informational
list into real archive candidates instead:

```
unleashctl diff  --context dev --service payments --archive-missing         # review candidates
unleashctl apply --context dev --service payments --archive-missing --yes   # archive them, one batch confirmation
unleashctl apply --context dev --service payments --archive-missing -i      # confirm/skip/abort one flag at a time
```

`-i`/`--interactive` and `--yes` are mutually exclusive. Without either,
`apply --archive-missing` prints the full candidate list and asks for one
confirmation covering the whole batch.

## Multiple repos, one instance

Point `--flags-dir` at whichever directory represents "this repo" — in
practice each repo just runs unleashctl against its own `flags/`, each with
its own `service` tag, all pointed at the same Unleash instance.

Scoping is enforced two ways: reads are filtered server-side to features
tagged with a service present in that directory, and `apply` hard-refuses
(no silent overwrite) if a local file's name collides with a remote feature
tagged with a *different* service, or with no service tag at all.

## Author contexts/*.yaml — custom context fields

[`docs/ContextField-spec.md`](./docs/ContextField-spec.md)

Unleash's *custom context fields* (`name`/`description`/`stickiness`/
`legalValues`) are a separate, unrelated concept from the
`context` command above (which manages CLI connection profiles) — the
naming collision is unfortunate but both terms are Unleash's own. Custom
context fields are global to an instance, not scoped per
project/environment/service like Feature flags, so there's no
`envOverride`/`contextOverride`/`links`/`tags` for this kind. Each field is
one file under `contexts/`:

```yaml
apiVersion: unleashctl/v1
kind: ContextField
metadata:
  name: subscriptionTier
spec:
  description: The user's subscription tier
  stickiness: true
  legalValues:
    - value: gold
      description: Gold tier
    - value: silver
```

`metadata.name` is immutable — Unleash has no rename endpoint, so renaming
it here creates a new field and orphans the old one (reported
informationally, or deleted with `--delete-missing`, same as below).

```
unleashctl context-fields diff  --context dev                  # what would change
unleashctl context-fields apply --context dev --yes             # apply it
unleashctl context-fields apply --context dev --dry-run          # print planned requests only
```

Same exit-code convention as `diff`/`apply` (`0` regardless of pending
changes, `1` on real error). There's no
batch import endpoint for context fields, so `apply` creates/updates each
one individually. `--delete-missing`, `--yes`, `-i`/`--interactive` work the
same way as Feature's `--archive-missing` (see above) — pass
`--delete-missing` to turn remote-only fields from informational into real
delete candidates.

## Author segments/*.yaml — segments

[`docs/Segment-spec.md`](./docs/Segment-spec.md)

Unleash *segments* (`name`/`description`/`project`/`constraints`) are
reusable constraint sets that can be attached to activation strategies.
Like custom context fields, segments are global to an instance (or scoped
to a single project via `project`), so there's no
`envOverride`/`contextOverride`/`links`/`tags` for this kind. Each segment
is one file under `segments/`:

```yaml
apiVersion: unleashctl/v1
kind: Segment
metadata:
  name: betaUsers
spec:
  description: Users opted into the beta program
  constraints:
    - contextName: userId
      operator: IN
      values:
        - user-1
        - user-2
```

Unlike context fields, Unleash's segment API is id-keyed
(`PUT`/`DELETE /api/admin/segments/{id}`) rather than name-keyed. `diff`/
`apply` still match local files to remote segments by `metadata.name`,
then resolve the id internally — renaming `metadata.name` still creates a
new segment and orphans the old one, same caveat as context fields.

```
unleashctl segments diff  --context dev                  # what would change
unleashctl segments apply --context dev --yes             # apply it
unleashctl segments apply --context dev --dry-run          # print planned requests only
```

Same exit-code convention as `diff`/`apply` (`0` regardless of pending
changes, `1` on real error). There's no batch import endpoint for
segments, so `apply` creates/updates each one individually.
`--delete-missing`, `--yes`, `-i`/`--interactive` work the same way as
above — pass `--delete-missing` to turn remote-only segments from
informational into real delete candidates.

## Regenerating API types

`internal/client/gen/types.gen.go` is generated from a live instance's
OpenAPI spec, scoped to just the schemas this project uses (full-spec
generation collides on inline-struct names). Regenerate against a running
instance:

```
make codegen                          # defaults to http://localhost:4242
UNLEASH_URL=https://dev... make codegen
```

## Testing

```
make test              # go build + go vet + go test ./...
```

`internal/client` also has an opt-in live integration test against a real
instance, skipped by default:

```
UNLEASHCTL_LIVE_URL=http://localhost:4242 UNLEASHCTL_LIVE_TOKEN=<token> \
  go test ./internal/client/... -run TestLive -v
```

## AI use disclosure

The bulk of this project (design implementation, codegen pipeline, tests,
CI workflow, and this README) was built with AI assistance (Claude Code),
working from the human-authored [`docs/archive/unleash-cli-tech-spec.md`](./docs/archive/unleash-cli-tech-spec.md).
Endpoint behavior and edge cases (e.g. tag-scoped export semantics, the
export/import payload shape) were verified against a live Unleash instance
and the upstream Unleash server source rather than assumed.
