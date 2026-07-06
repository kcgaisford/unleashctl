# unleashctl

A Go CLI wrapping the Unleash Admin API, built on Cobra + Viper. Full design
in [`unleash-cli-tech-spec.md`](./unleash-cli-tech-spec.md).

**Status**: Phase 1 implemented — `unleashctl diff` and `unleashctl apply`,
built on contexts (§2) and `flags/*.yaml` desired-state files (§5.1). CRUD
commands, `export`/`import`, and `sync` are not built yet (see the spec's
§9 open questions).

## Prerequisites

- Go 1.26+
- A running Unleash instance (OSS) — [Docker quick start](https://docs.getunleash.io/quickstart)
- An Admin API token (personal access token or service account token) for that instance

## Build

```
git clone <this repo>
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

## Author flags/*.yaml

Each feature is one file under `flags/`, with a `metadata.service` tag that
becomes a real `service:<name>` tag on the Unleash feature — this is what
scopes `diff`/`apply` when multiple repos share one OSS instance (spec §6.4).

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
  strategies:
    - name: flexibleRollout
      parameters: { rollout: "25", stickiness: userId, groupId: new-checkout }
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
identical on every instance. See spec §5.1 for the full resolution rule.

## Usage

```
unleashctl diff --context dev                       # what would change
unleashctl apply --context dev --yes                # apply it
unleashctl diff  --context dev --service payments    # scope to one service
unleashctl apply --context dev --dry-run             # print the request payload only
```

Exit codes (Terraform convention): `0` no changes, `2` changes pending,
other non-zero on error — handy for CI gating.

`diff`/`apply` are additive-only by default: a remote feature tagged with a
service but missing a local file is reported informationally, never treated
as a delete (`--archive-missing` from the spec isn't built yet).

## Multiple repos, one instance

`flags_other_repo/` in this repo simulates a *second* repo pointed at the
same Unleash instance, with its own directory and its own `service` tag
(spec §6.4). Point `--flags-dir` at whichever directory represents "this
repo" — in practice each repo just runs unleashctl against its own `flags/`:

```
unleashctl diff --context dev --flags-dir flags_other_repo
```

Scoping is enforced two ways: reads are filtered server-side to features
tagged with a service present in that directory, and `apply` hard-refuses
(no silent overwrite) if a local file's name collides with a remote feature
tagged with a *different* service, or with no service tag at all.

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
working from the human-authored [`unleash-cli-tech-spec.md`](./unleash-cli-tech-spec.md).
Endpoint behavior and edge cases (e.g. tag-scoped export semantics, the
export/import payload shape) were verified against a live Unleash instance
and the upstream Unleash server source rather than assumed.
