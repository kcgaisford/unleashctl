# unleashctl — Technical Spec

## 1. Overview

`unleashctl` — a Go CLI wrapping the Unleash Admin API, built on
**Cobra** (command tree) + **Viper** (config/flags/env binding). The
name and shape are deliberately kubectl-flavored: `contexts` (§2) work
like `kubectl config` contexts, and `flags/*.yaml` (§5) plays the same
role as Kubernetes manifests — a directory of desired-state YAML that
`diff`/`apply` reconciles against a live cluster (here, a live Unleash
instance). Two layers:

1. **Imperative layer** — direct CRUD wrapper over the Admin API (projects,
   features, strategies, environments, segments, tags, variants), plus
   export/import.
2. **Declarative/GitOps layer** — a single set of desired-state files
   checked into git (`flags/*.yaml`). There is no separate "migration
   file" format — a migration is just a git-tracked change to those same
   files. Two commands, split cleanly by direction:
   - `diff` / `apply` — **files → instance.** Comparing live remote state
     directly against the resolved `flags/*.yaml`. Ordering (development
     before production) is left to the CI/CD pipeline, not the CLI — the
     CLI just answers "what's pending for this target" and applies it
     when asked. This is the only way `flags/*.yaml` ever gets pushed to
     an instance.
   - `sync` — **instance → instance**, exclusively. Direct, unordered
     comparison/reconciliation between two live Unleash instances (e.g.
     "is stage caught up with dev," seeding a local dev instance). Never
     touches local files.

Non-goal: this is not a general Unleash Admin UI replacement — no
user/RBAC management in v1 (see §11).

### Scope: built for Unleash OSS

This targets **open-source Unleash**, not Enterprise. Concretely:

- **One project**: OSS is effectively single-project (`default`).
  There's no CRUD for creating/deleting projects — the CLI just assumes
  `default` everywhere and doesn't ask.
- **Two environments**: `development` and `production` — the OSS
  defaults. No environment create/delete; the CLI treats these two names
  as fixed. (If someone has renamed or added environments on their
  instance, `--environment` still accepts any string — the CLI just
  doesn't assume more than two exist by default.)
- **No change requests**: that's Enterprise-only, so the note in §4 about
  detecting a draft change request is a "just in case," not something
  the CLI's UX is built around.
- Segments and custom strategies are OSS features and stay in scope.

The one thing OSS *doesn't* give us for free is per-team isolation —
everyone shares the same single project. Since **multiple git repos
hooking into the same instance** is an explicit goal, isolation is
handled through a `service` tag that lives on the Unleash instance itself
(§5.1, §6.4) rather than anything invented purely on the CLI side — the
scoping piggybacks on Unleash's own tag feature, so the instance stays
the source of truth and is self-describing even if you never had the
CLI's config in front of you.

---

## 2. Architecture

```
cmd/
  root.go              # cobra root, global flags, viper init
  config.go            # `unleashctl config` (contexts/profiles)
  project.go           # `unleashctl project get` (always `default`)
  features.go          # `unleashctl features ...`
  strategies.go
  environments.go
  segments.go
  tags.go
  export.go / import.go
  sync.go              # `unleashctl sync diff|apply` (instance ↔ instance ONLY)
  diff.go / apply.go   # `unleashctl diff|apply` (files → instance)
internal/
  client/              # thin typed Admin API client (net/http, retries, auth)
  state/               # flags/*.yaml model + (de)serialization + env resolution
  ownership/           # service-tag scoping + conflict detection for shared instances (§6.4)
  differ/              # state diffing (features/strategies/etc.), shared by
                       # both sync and diff/apply
  render/              # table/json/yaml output formatting
config/
  schema for ~/.unleashctl/config.yaml
```

### Config precedence (Viper)
flags > env vars (`UNLEASHCTL_*`) > project-local `.unleashctl.yaml` >
global `~/.unleashctl/config.yaml` > defaults.

### Contexts (kubectl-style)

The realistic OSS topology is usually **separate instances, not one
shared instance with multiple environments switched on**. Each Unleash
instance typically has just one environment enabled, and the same
environment *name* can be reused across instances that don't share any
data — e.g. a `dev` instance and a `stage` instance can both have only
`development` enabled, while `prod` has only `production` enabled. A
context therefore usually maps to exactly one environment, so it's worth
recording that pairing once instead of passing `--environment` on every
call:

```yaml
current-context: dev
contexts:
  - name: dev
    url: https://dev-unleash.internal
    environment: development       # this instance only has one enabled
    token-env: UNLEASH_DEV_TOKEN
  - name: stage
    url: https://stage-unleash.internal
    environment: development       # same env *name* as dev, different instance/data
    token-env: UNLEASH_STAGE_TOKEN
  - name: prod
    url: https://prod-unleash.internal
    environment: production
    token-env: UNLEASH_PROD_TOKEN
    # allow_sync: false is implied by the name `prod` — shown here only for clarity
  - name: local
    url: http://localhost:4242     # a developer's own local instance
    environment: development
    token-env: UNLEASH_LOCAL_TOKEN
```

`allow_sync` gates whether `sync` — instance-to-instance only, §5.2 —
is allowed to write to a context at all. It **defaults to `false`
automatically for any context named `prod` or `production`**
(case-insensitive) — the CLI assumes those should only ever be changed
through the git-tracked `diff`/`apply` path (§6) unless told otherwise,
so the safe behavior doesn't depend on remembering to set it. Every
other context defaults to `true`. Either default can be overridden
explicitly with `allow_sync: true`/`false` in config, e.g. if a team
names their production context something else, or deliberately wants
`sync` writes allowed against a context literally named `prod`. It only
blocks `sync` as a **write target** (`--to-context`); reading from it
(`--from-context`) is unaffected, since comparing against prod is
exactly the kind of ad hoc check `sync diff` is for. `diff`/`apply` (§6)
is unaffected by this flag entirely — it's a `sync`-specific guardrail,
and `sync` never touches local `flags/*.yaml` files at all (§5.2), so
there's no "files case" for it to gate anymore.

`--environment` on any command defaults to the current context's
configured `environment` if set, and is still overridable/required
explicitly for a context that doesn't declare one (e.g. a genuinely
shared instance with multiple environments enabled — the model from the
previous draft of this doc still works, it's just the less common case).
(There's no `project` field on a context at all — OSS only ever has
`default`.)

Note that `dev` and `stage` both use the environment name `development`
but are otherwise unrelated — each instance's tags live in that
instance's own storage, so there's no cross-instance collision just
because the environment name matches.

`--context` flag overrides `current-context` per invocation. Tokens are
resolved from env var / OS keyring, never persisted in plaintext config.
Every command accepts `--context`, `--url`, `--token` (flag > env > context)
so CI pipelines can run without a config file at all.

Auth: support **service account tokens** (recommended for automation/CI) and
**personal access tokens**; both are just bearer tokens against the Admin
API, so the client layer doesn't need to distinguish them.

### Identity lives in the flags themselves

There's no separate repo-identity config. Instead, each flag file declares
its own `service` in `metadata` (§5.1), and that becomes a real tag on the
Unleash feature — visible in the Unleash UI, not just a CLI-internal
convention. Scoping and ownership/conflict checks (§6) all key off that
tag rather than anything stored only in this repo.

---

## 3. Imperative CRUD commands

Pattern: `unleashctl <resource> <verb> [flags]`, verbs = `list|get|create|update|delete`
plus resource-specific extras. Output via `--output table|json|yaml` (default table).

| Resource | Verbs | Notes |
|---|---|---|
| `project` | get | OSS has exactly one project (`default`) — no list/create/update/delete |
| `features` | list, get, create, update, delete, archive, revive | always scoped to `default`; `--tag` filter on list |
| `features strategies` | list, add, update, remove | scoped to `--feature --environment` |
| `features enable` / `disable` | — | per environment: `--feature --environment` |
| `features variants` | list, set | variant flag payloads |
| `environments` | list, get | OSS ships `development`/`production` fixed — no create/delete |
| `segments` | list, get, create, update, delete | |
| `tags` | list, create, delete, attach, detach | also used internally for `service` tagging, §6.4 |
| `strategies` (custom) | list, get, create, update, delete | custom strategy definitions |
| `api-tokens` | list, create, revoke | for bootstrapping automation (careful with perms) |

Every mutating command supports `--dry-run` (print the request payload,
no network call) and `--yes` to skip interactive confirmation in scripts.

### Example
```
unleashctl features create --name new-checkout --type release
unleashctl features strategies add --feature new-checkout \
  --environment production --strategy flexibleRollout --params rollout=25
unleashctl features enable --feature new-checkout --environment production
```

---

## 4. Export / Import

Two tiers, mirroring what Unleash itself exposes:

1. **Full-instance export/import** — thin wrapper over
   `/api/admin/state/export` and `/api/admin/state/import`
   (`--format json|yaml`, `--include-strategies`, `--include-feature-toggles`,
   `--drop-before-import`, `--keep-existing`). Good for backup/restore and
   whole-instance cloning.
2. **Scoped export/import** — wraps the newer project/environment-scoped
   import-export flow (validate-then-import, respecting segments/custom
   strategies/dependencies that must already exist on the target). This is
   what both `sync` and `diff`/`apply` (§5, §6) build on, since it's safer
   for partial, repeated changes than a full-state drop.

```
unleashctl export --context prod --output prod.yaml
unleashctl import --context prod --input prod.yaml --validate-only
unleashctl import --context prod --input prod.yaml
```

`--validate-only` calls the validation endpoint first and prints
errors/warnings/required-permissions without applying anything — used
automatically by both `sync diff`/`sync apply` and `diff`/`apply` (§5,
§6).

Note: Unleash will open a **change request** instead of applying directly
if change requests are enabled on the target environment (Enterprise). The
CLI should detect this (response indicates a draft CR was created) and
print the CR id/link rather than reporting a hard failure.

---

## 5. Desired-state files (`flags/*.yaml`)

This is the **one and only** authored format. There is no separate
migration-file format — a migration is just a git-tracked change to
these files that `diff` (§6) picks up the next time it compares live
instance state against them. This is the core "same paradigm" idea: you
always edit a feature's desired state in the same place, whether you're
fixing drift right now or promoting a change through environments over
the next week.

### 5.1 Schema — consistent by default, two levels of override

The default assumption is that a flag's configuration is **the same
everywhere**. `spec` is that shared definition. Two optional override
blocks sit on top of it, for two different kinds of exception:

- **`envOverride`**, keyed by **environment name** (`development`/
  `production`) — applies to *every* context configured with that
  environment. Useful because `dev` and `stage` often should genuinely
  behave the same for testing purposes (§2: they're both `development`)
  — one `envOverride.development` block covers both at once instead of
  duplicating it per context.
- **`contextOverride`**, keyed by **context name** (`dev`/`stage`/
  `prod`/...) — applies to exactly one instance, and wins over
  `envOverride` where both touch the same field. This is for the case
  where `dev` and `stage` *shouldn't* match despite sharing an
  environment name.

```
flags/
  new-checkout.yaml
  refunds-v2.yaml
  kill-switch-maintenance.yaml
```

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

`metadata.service` is applied to the underlying feature as an Unleash tag
(type `service`, value `payments`) on create — a real, visible artifact on
the instance, not something only this CLI/repo knows about. It's the
single scoping mechanism used throughout §5.2 and §6: which features a
given `sync`/`diff`/`apply` invocation considers, and how conflicts
between repos are detected. Strongly
recommended on every flag; a flag with no `service` is legal but opts out
of all of that scoping (see §6.4).

Resolution rule: for a given `(feature, context)` pair, start from
`spec`, shallow-merge any keys under `envOverride.<environment-name>`
(the context's configured environment, §2), then shallow-merge any keys
under `contextOverride.<context-name>` on top of that (arrays like
`strategies` are replaced wholesale at each step, not merged
element-wise — simpler to reason about and matches how Unleash treats a
strategy list per environment). A feature with neither override block is,
by construction, identical everywhere — this should be the common case,
and lint/review tooling can flag either block as "worth double-checking
why this differs."

This also covers the less-common shared-instance case (one context, both
`development` and `production` enabled, §2) with no special handling —
`envOverride` is keyed by environment name regardless of how many
contexts exist, so it disambiguates correctly whether that context is the
only one using an environment name or one of several.

`links` and `tags` (both optional, top-level, siblings of `metadata`/
`spec`) are declared once per feature and always apply regardless of
environment or context — unlike `spec`, they don't participate in
`envOverride`/`contextOverride` resolution, since neither concept is
environment-scoped in Unleash itself. `tags` is in addition to the
automatic `service` tag described above — both end up applied to the
feature.

### 5.2 `sync` — instance ↔ instance, exclusively

`sync` never touches local `flags/*.yaml` files. It's for comparing or
reconciling two *live* Unleash instances directly — "is stage actually
caught up with dev," seeding a local dev instance, that kind of thing.
Files → instance is handled entirely by `diff`/`apply` (§6); `sync` isn't
a second way to do that.

No git history, no state tracking, no service-file authoring required.
`sync` doesn't remember anything between runs — it's a pure live
comparison every time. It's the imperative, cluster-to-cluster escape
hatch; `diff`/`apply` is the only GitOps-tracked path, and the only one
that ever reads `flags/*.yaml`.

Just two verbs — `diff` (read-only) and `apply` (mutating). `apply`
always resolves and prints the same comparison `diff` would show before
doing anything, and stops there unless confirmed (`--yes` to skip the
prompt in CI).

- `unleashctl sync diff --from-context dev --to-context stage` —
  fetches both instances' state and diffs them directly. This is the
  normal way to answer "is stage actually in sync with dev right now,"
  independent of whatever's committed in any repo.
- `unleashctl sync apply --from-context dev --to-context stage [--yes]`
  — pushes `dev`'s state onto `stage`. Deletions require
  `--allow-destroy` to even show (validate-then-import under the hood).
- A local developer instance is just another context —
  `unleashctl sync apply --from-context dev --to-context local` seeds a
  developer's own Unleash (e.g. `http://localhost:4242`) from the shared
  `dev` instance. Nothing special about "local" beyond it being a context
  whose URL happens to be on the developer's machine.

`apply` will refuse to write to a context whose config sets
`allow_sync: false` (§2) as `--to-context` — this is the main guardrail
for keeping `sync` away from instances that should only ever be changed
through the git-tracked `diff`/`apply` path. Using a sync-disabled
context as `--from-context` is still fine — comparing against it is
harmless, only writing to it is blocked.

**`--service payments`** narrows either verb to just features tagged
`service:payments` on both instances — the ad hoc review case: check
just one service's flags between two clusters without touching or
fetching anything outside that scope. With no `--service`, `sync`
compares/copies everything (full cluster comparison), which is the point
of the dev-vs-stage use case.

`sync` is intentionally history-unaware and idempotent — safe to run on
a schedule to catch instance-vs-instance drift. Bootstrapping the very
first `flags/*.yaml` files from a live instance is `export`'s job (§4),
not `sync`'s — `sync` has nothing to do with local files at all.

Environment scoping follows whatever's actually enabled on each side —
queried live from `/api/admin/environments`, not assumed. For the
`dev`/`stage`/`prod` topology (§2, one environment each), there's nothing
to disambiguate. For the less-common case of an instance with more than
one environment enabled, `--environment` narrows the comparison to a
single one on both sides; without it, `sync` compares every environment
both instances have in common.

---

## 6. `diff` / `apply` — files → instance

This is the "migrations" concept, but it's just a comparison against the
same `flags/*.yaml` files — there's nothing extra to author. A change is
"a migration" simply by virtue of being a committed diff to those files.
**Ordering across environments (development before production) is a
CI/CD pipeline concern, not something the CLI enforces** — the CLI's job
is just "tell me what's pending for this target" and "apply it," scoped
to whichever target the pipeline stage is currently running against.

**By default, `diff`/`apply` are additive-only: create and update,
never archive.** A file missing from `flags/` is never treated as "this
flag should be removed from the instance." This matters specifically
because multiple MRs can be open against the same `service` scope at
once — an MR's branch or a rebase in flight can make a flag *look*
locally absent for reasons that have nothing to do with anyone intending
to remove it, and a prune-by-default `diff` would misread that as a
delete. Archiving is opt-in and explicit (`--archive-missing`, below),
never a side effect of "I don't see this file right now."

`--context` says *which instance* a command runs against; `--environment`
says which environment on it — and per §2, that usually just comes from
the context's own config rather than being passed every time, since each
instance in a typical OSS setup only has one enabled.

### 6.1 Commands

```
unleashctl diff  --context prod                              # environment implied by context config (§2)
unleashctl apply --context prod [--yes]                       # apply the diff
unleashctl diff  --context prod --service payments            # scope to one service only
unleashctl diff  --context prod --service payments --archive-missing  # + drift-correct: archive
                                                                        # remote flags in this service
                                                                        # scope with no local file
unleashctl apply --context prod --service payments --archive-missing -i  # same, but confirm
                                                                          # each candidate one by one
```

`--environment` defaults to whatever the context declares (§2); it's only
something you must pass explicitly for a context that doesn't declare
one (a shared multi-environment instance). Either way there's no silent
"just pick one" behavior — it's either unambiguous from the context or
required on the command line. `--service` is optional; without it, the
command covers every service represented in `flags/`, diffing/applying
each one independently.

`diff` computes, per service present in scope: fetch remote features
tagged with that service, resolve the matching local files (§5.1,
applying `envOverride`/`contextOverride`), and diff the two directly —
exactly `sync`'s comparison, through the same `internal/state` +
`internal/differ` machinery, just restricted to this service's tagged
features. The result is a reviewable set of **create/update** operations
— this is functionally "the migration," generated from a live comparison
rather than hand-authored. A remote feature tagged with this service
that has no matching local file is surfaced as informational only ("N
flag(s) in this service have no local file — not archiving; rerun with
`--archive-missing` to review") unless `--archive-missing` is passed.
`apply` runs that same comparison (validate-then-import scoped to that
environment). There's no built-in cross-target sequencing or approval
gate — a typical CI setup calls `unleashctl apply --context dev` in the
dev deploy stage, `unleashctl apply --context stage` in the staging
deploy stage, and `unleashctl apply --context prod` in the prod deploy
stage, with the pipeline's own stage ordering/manual-approval gates (not
the CLI) deciding when each later stage is allowed to run.

**`--archive-missing`** requires `--service` (same reasoning as before —
scope can't be inferred from local files once the last file for a
service is gone): within the diff's already-fetched remote/local
comparison for that service, propose archiving any remote feature with
no local counterpart, instead of just reporting it informationally. This
is the explicit drift-correction/cleanup path — regular `diff`/`apply`
will never do this on its own. Ownership scoping (§6.4) still applies:
only features tagged with the given service are ever candidates, so this
can't reach into another repo's flags even by accident.

Confirmation is batch by default: `apply --archive-missing` prints the
full list of candidate flags up front and asks for one confirmation
covering the whole batch (or proceeds unattended with `--yes`, e.g. in
CI). Pass `-i`/`--interactive` to go through the list one flag at a
time instead — confirm, skip, or abort per flag — for the case where
someone actually wants to eyeball each one rather than trust the list as
a whole. `-i` and `--yes` are mutually exclusive.

Exit codes: `0` whether or not changes are pending — drift alone is
never a failure, so a diff-only CI job doesn't break just because
something changed. `1` only on a real error (fetch failure, or a refused
ownership conflict, §6.4). See `docs/Feature-spec.md`.

### 6.2 Drift detection

Drift detection isn't a separate mode — it's just what `diff` does,
always, because it always compares live remote state directly rather
than inferring "what should have changed" from history. If someone
hand-edited a strategy in the Unleash UI last week, the very next `diff`
sees it, the same way it'd see a change that came from an actual commit.
No separate `verify` command, and no need to distinguish "expected" drift
from "unexpected" drift, since there's only one code path either way.

### 6.3 `sync` vs `diff`/`apply` — when to use which

- **`sync`**: instance-to-instance only (§5.2) — use for whole-cluster
  comparisons (e.g. "is stage actually caught up with dev"), seeding a
  local dev instance, or checking one instance against another ad hoc.
  Never touches local `flags/*.yaml`.
- **`diff`/`apply`**: files-to-instance only — the standard path for
  dev → stage → prod GitOps workflows, where CI decides the ordering.
  This is the *only* command that ever reads `flags/*.yaml` and writes
  it to an instance.

The two share the exact same underlying diff/apply primitives in
`internal/state` + `internal/differ` — the only difference is which side
of the comparison is a live instance versus another live instance
(`sync`) or a local file tree (`diff`/`apply`).

### 6.4 Multiple repos, one instance

OSS's single `default` project means there's no built-in wall between
"payments' flags" and "checkout's flags" on a shared instance —
everything lives in the same namespace. The `service` tag (§5.1) is what
stands in for that wall. This section is mostly about `diff`/`apply`,
since that's the only command that writes `flags/*.yaml` to an instance;
`sync` gets the same treatment for its own instance-to-instance case.

- **Scoped reads**: `diff`'s remote-state fetch is filtered to features
  tagged with a service present in the local `flags/` dir (or named by
  `--service`). A feature tagged with some other service never appears
  in this repo's diff at all — not as a pending change, not as a delete
  candidate.
- **Hard refusal on mismatched or untagged features**: if a local
  `flags/*.yaml` file's `metadata.name` matches a remote feature that's
  either (a) tagged with a *different* `service` than the local file
  declares, or (b) untagged (pre-existing, not yet CLI-managed), `apply`
  refuses with a clear error rather than silently re-tagging or
  overwriting it.
- **`--adopt`**: the explicit escape hatch. `unleashctl apply --adopt
  new-checkout` claims an untagged or mismatched feature by writing the
  `service` tag the local file declares, after an interactive
  confirmation (or `--yes` in CI, used deliberately, not as a default).
  `sync apply` has the equivalent `--adopt` for its instance-to-instance
  case, claiming a feature on the target instance that's untagged or
  differently tagged than the source.
- **Deletions/archiving are likewise scoped**: `sync`'s `--allow-destroy`
  and `apply`'s `--archive-missing` (§6.1) both only ever touch features
  tagged with a service in scope — neither can reach a feature owned by
  a different service/repo, even when explicitly asked to clean up.

Two repos can point `--context dev` (or `stage`, or `prod`) at the same
instance, each with its own `flags/` directory, and as long as they
declare different `service` values (the common case — one service, one
owning repo), neither one's `diff`/`apply` will ever propose a change to
a flag it doesn't own. If two repos legitimately share a service (e.g. a
platform team's repo and a product team's repo both touch `payments`),
`diff` for either one naturally reflects whatever the other last applied
— which is correct: the comparison is always against the service's
actual state on the instance, not either repo's private view of it.

---

## 7. Example end-to-end workflow

```
# 1. Bootstrap: seed flags/*.yaml from an existing instance, then
#    bring it under diff/apply's management
unleashctl export --context dev -o /tmp/dev.yaml
# (split /tmp/dev.yaml into flags/*.yaml by hand, or a small conversion script)
unleashctl diff  --context dev   # should show no-ops if the split was faithful
unleashctl apply --context dev --yes

# 2. Day-to-day: edit desired state directly, review the diff in the PR itself
$EDITOR flags/new-checkout.yaml   # e.g. bump rollout to 25%, add contextOverride
git commit -am "Roll new-checkout out to 25% in production"
# CI, dev deploy stage:
unleashctl apply --context dev --yes
# CI, stage deploy stage:
unleashctl apply --context stage --yes
# CI, prod deploy stage (pipeline's own gate/approval decides when this stage runs):
unleashctl apply --context prod --yes

# 3. Periodic drift check (same command, no separate verify step)
unleashctl diff --context prod   # exit 0 even if drifted; exit 1 only on real errors

# 4. Ad hoc, outside the git-tracked flow entirely: is stage actually
#    caught up with dev right now? (whole-cluster comparison, no flags/
#    files involved)
unleashctl sync diff --from-context dev --to-context stage

# 5. A developer seeds their own local Unleash from the shared dev instance
unleashctl sync apply --from-context dev --to-context local --yes

# 6. Explicit cleanup: `payments` retired a flag months ago and its file
#    is long gone from flags/ — actually archive it now, deliberately
unleashctl diff  --context prod --service payments --archive-missing
unleashctl apply --context prod --service payments --archive-missing --yes
```

---

## 8. `ContextField` spec kind — custom context fields

Unleash's *custom context fields* (`name`/`description`/`stickiness`/
`legalValues`, see Unleash's own context-field docs) are an
entirely separate concept from the `context` word used everywhere else in
this spec (§2's kubectl-style CLI connection profiles) — they happen to
share a name with no other relationship. Unlike `Feature`, this kind is
global to an instance: no project/environment/service scoping, so no
`envOverride`/`contextOverride`/`links`/`tags`, and no `--service` flag on
its commands.

One file per field, under `contexts/*.yaml`:

```yaml
apiVersion: unleashctl/v1
kind: ContextField
metadata:
  name: subscriptionTier   # immutable — no rename endpoint; renaming here
                            # creates a new field and orphans the old one
spec:
  description: The user's subscription tier
  stickiness: true
  legalValues:
    - value: gold
      description: Gold tier
```

`sortOrder` (also part of Unleash's context-field API) is deliberately left
out of this spec: Unleash's own UI never exposes it for editing (the
create/edit form has no such field, and the list view's `sortOrder` column
is hidden by default), and its update endpoint silently discards/resets it
server-side — not a field worth managing declaratively.

Commands, grouped under `context-fields` to avoid colliding with the
existing `context` command:

```
unleashctl context-fields diff  --context dev                  # what would change
unleashctl context-fields apply --context dev --yes             # apply it
unleashctl context-fields apply --context dev --dry-run          # print planned requests only
unleashctl context-fields apply --context dev --delete-missing --yes   # + delete remote-only fields
```

Same exit-code convention as §6 (`0` regardless of pending changes, `1`
on real error). There's no batch import/export endpoint for context
fields (unlike Feature's `features-batch/*`), so `apply` calls the Admin
API's create/update/delete endpoints individually per field, one
request per change. See `docs/ContextField-spec.md`.

---

## 9. `Segment` spec kind — segments

Unleash *segments* (`name`/`description`/`project`/`constraints`, see
Unleash's own segment docs) are reusable constraint sets that can be
attached to activation strategies. Like `ContextField`, this kind is
global to an instance (or scoped to a single project via `project`): no
`envOverride`/`contextOverride`/`links`/`tags`. Unlike `ContextField`,
Unleash's segment API is id-keyed (`PUT`/`DELETE /api/admin/segments/{id}`)
rather than name-keyed, so `diff`/`apply` match local files to remote
segments by `metadata.name` and resolve the id internally to act on
updates/deletes.

One file per segment, under `segments/*.yaml`:

```yaml
apiVersion: unleashctl/v1
kind: Segment
metadata:
  name: betaUsers   # matched by name; renaming here creates a new segment
                     # and orphans the old one, same caveat as ContextField
spec:
  description: Users opted into the beta program
  constraints:
    - contextName: userId
      operator: IN
      values:
        - user-1
        - user-2
```

`constraints` may be omitted or empty (matches every user).

Commands, grouped under `segments`:

```
unleashctl segments diff  --context dev                  # what would change
unleashctl segments apply --context dev --yes             # apply it
unleashctl segments apply --context dev --dry-run          # print planned requests only
unleashctl segments apply --context dev --delete-missing --yes   # + delete remote-only segments
```

Same exit-code convention as §6 (`0` regardless of pending changes, `1`
on real error). There's no batch import/export endpoint for segments
(unlike Feature's `features-batch/*`), so `apply` calls the Admin API's
create/update/delete endpoints individually per segment, one request per
change. See `docs/Segment-spec.md`.

---

## 10. Implementation notes

- **HTTP client**: `internal/client` wraps `net/http` with retry/backoff
  (429/5xx), consistent auth header injection, and typed request/response
  structs generated or hand-written from Unleash's OpenAPI spec (Unleash
  publishes one — worth codegen'ing the model structs even if the
  higher-level client is hand-written).
- **Output**: shared `internal/render` package for table (default,
  human-friendly), `json`, `yaml` — every list/get command supports all
  three via `--output`.
- **Testing**: unit tests mock `internal/client` at the interface level;
  integration tests run against a disposable Unleash instance (official
  Docker image) in CI for `export/import`, `sync`, and `diff`/`apply`
  end-to-end paths, including a two-repo/shared-service scenario to
  verify ownership scoping behaves correctly whether services are
  disjoint or shared.
- **Concurrency**: running `diff`/`apply` (or `sync`) against several
  contexts is safe to parallelize, since each hits a different instance
  with no shared state between them; any sequencing between environments
  is the CI pipeline's responsibility.
- **Secrets**: never write tokens to config files or logs; redact
  Authorization headers in `--verbose`/debug output.

---

## 11. Open questions / v2 candidates

- User/role management commands (out of scope v1).
- Change-request-aware `apply` (auto-detect CR creation, poll for
  approval, or fail fast with a clear message) — Enterprise-only feature,
  needs a decision on how much the CLI should special-case it.
- No audit trail: `diff`/`apply` compares live state on every run with no
  memory of past applies, so there's no built-in answer to "what was
  applied to prod, and when" beyond whatever the CI system's own job logs
  capture. Worth deciding whether that's sufficient or whether some
  lightweight record is worth adding later — deliberately left out for
  now to keep the mechanism simple.
- Segments now have first-class CRUD via the `Segment` spec kind (§9).
  Custom strategies are still treated as "must already exist on target"
  per Unleash's own import rules — may need their own representation too
  if teams want them GitOps-managed as well.
- Ownership/service tags (§6.4) are a convention layered on top of
  Unleash's tag feature, not something Unleash enforces — nothing stops
  someone from manually deleting or changing a `service:*` tag outside
  the CLI. Worth deciding whether `diff` should treat a missing-or-changed
  tag as a loud warning (likely) versus silently re-tagging.
- If someone's OSS instance has been reconfigured with environment names
  other than `development`/`production` (Unleash allows renaming), the
  CLI's `--environment` flag still works with any string, and `sync`
  already resolves environments live from `/api/admin/environments`
  rather than assuming a hardcoded pair (§5.2) — so this is mostly
  handled, just worth confirming `diff`/`apply` do the same when a
  context doesn't declare a fixed `environment` in config.
- Name-based defaults (`allow_sync: false` for anything named `prod`/
  `production`, §2) are convenient but implicit — worth a loud one-time
  notice (e.g. on first `config` setup, or in `sync`'s output) so nobody
  is surprised that naming a context `prod` silently changed its
  behavior, especially the direction of surprise where someone expects
  `sync` to work and it quietly refuses.
- `--archive-missing` (§6.1) requires `--service`, but doesn't yet say
  what happens if the service was never fully removed — just partially
  (some flags moved to a different service tag, say). Worth confirming
  the archive candidate list is always "tagged with this service AND no
  local file with this exact name," not anything broader, so a rename
  doesn't get misread as a delete-then-recreate.
