# `ContextField` manifest spec

Kind: `ContextField` — the desired-state file for one Unleash *custom
context field* (`name`/`description`/`stickiness`/`legalValues`, see
Unleash's own context-field docs). This is an entirely separate concept
from the `context` word used everywhere else in `archive/unleash-cli-tech-spec.md`
(the kubectl-style CLI connection profiles) — they happen to share a
name with no other relationship.

Unlike `Feature`, this kind is **global to an instance**: no
project/environment/service scoping, so no
`envOverride`/`contextOverride`/`links`/`tags`, and no `--service` flag
on its commands.

## Schema

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

`sortOrder` (also part of Unleash's context-field API) is deliberately
left out of this spec: Unleash's own UI never exposes it for editing
(the create/edit form has no such field, and the list view's
`sortOrder` column is hidden by default), and its update endpoint
silently discards/resets it server-side — not a field worth managing
declaratively.

## Commands

Grouped under `context-fields` to avoid colliding with the existing
`context` command:

```
unleashctl context-fields diff  --context dev                  # what would change
unleashctl context-fields apply --context dev --yes             # apply it
unleashctl context-fields apply --context dev --dry-run          # print planned requests only
unleashctl context-fields apply --context dev --delete-missing --yes   # + delete remote-only fields
```

There's no batch import/export endpoint for context fields (unlike
`Feature`'s `features-batch/*`), so `apply` calls the Admin API's
create/update/delete endpoints individually per field, one request per
change.

## Exit codes

Same convention as `Feature` (see `docs/Feature-spec.md`): `diff` exits
`0` whether or not fields have pending changes — drift alone is not a
failure. Exit `1` only on a real error (fetch/apply failure).
