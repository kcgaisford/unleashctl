# `Segment` manifest spec

Kind: `Segment` — the desired-state file for one Unleash *segment* (a
named, reusable set of constraints — `name`/`description`/`project`/
`constraints`, see Unleash's own segment docs) that can be attached to
activation strategies.

Like `ContextField`, this kind is **global to an instance** (or scoped to
a single project via `project`, but never per-environment/service): no
`envOverride`/`contextOverride`/`links`/`tags`, and no `--service` flag on
its commands.

One real divergence from `ContextField`: Unleash's segment API is
**id-keyed**, not name-keyed (`PUT`/`DELETE /api/admin/segments/{id}`),
because segment names aren't part of the URL. `diff`/`apply` still match
local files to remote segments by `metadata.name` (same as ContextField),
then resolve the matched segment's id internally to call update/delete.

## Schema

One file per segment, under `segments/*.yaml`:

```yaml
apiVersion: unleashctl/v1
kind: Segment
metadata:
  name: betaUsers   # matched against the remote segment's name; renaming
                     # here creates a new segment and orphans the old one
spec:
  description: Users opted into the beta program
  constraints:
    - contextName: userId
      operator: IN
      values:
        - user-1
        - user-2
```

`constraints` may be omitted or empty — an empty-constraint segment
matches every user. `project`, when set, scopes the segment to a single
project instead of making it available instance-wide.

## Commands

Grouped under `segments`:

```
unleashctl segments diff  --context dev                  # what would change
unleashctl segments apply --context dev --yes             # apply it
unleashctl segments apply --context dev --dry-run          # print planned requests only
unleashctl segments apply --context dev --delete-missing --yes   # + delete remote-only segments
```

There's no batch import/export endpoint for segments (unlike `Feature`'s
`features-batch/*`), so `apply` calls the Admin API's create/update/delete
endpoints individually per segment, one request per change.

## Exit codes

Same convention as `ContextField` (see `docs/ContextField-spec.md`):
`diff` exits `0` whether or not segments have pending changes — drift
alone is not a failure. Exit `1` only on a real error (fetch/apply
failure).
