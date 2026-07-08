"""Filter api/openapi.json down to the schemas this project's codegen needs
(plus their transitive $refs), writing api/openapi.min.json. Full-spec
generation collides on inline-struct names across the Unleash OpenAPI spec's
242 schemas; scoping to just what's used avoids that. Run via `make codegen`.
"""
import json
import re
import sys

spec = json.load(open("api/openapi.json"))
schemas = spec["components"]["schemas"]

roots = [
    "featureSchema", "featureStrategySchema", "featureEnvironmentSchema",
    "tagSchema", "environmentSchema", "environmentsSchema",
    "importTogglesSchema", "importTogglesValidateSchema", "importTogglesValidateItemSchema",
    "exportResultSchema", "exportQuerySchema",
    "contextFieldSchema", "contextFieldsSchema",
    "createContextFieldSchema", "updateContextFieldSchema",
    "upsertSegmentSchema", "adminSegmentSchema", "segmentsSchema",
]

ref_re = re.compile(r"#/components/schemas/([A-Za-z0-9_]+)")


def find_refs(obj):
    found = set()
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k == "$ref" and isinstance(v, str):
                m = ref_re.search(v)
                if m:
                    found.add(m.group(1))
            else:
                found |= find_refs(v)
    elif isinstance(obj, list):
        for item in obj:
            found |= find_refs(item)
    return found


selected = set()
queue = list(roots)
while queue:
    name = queue.pop()
    if name in selected:
        continue
    if name not in schemas:
        print(f"WARNING: {name} not found in schemas", file=sys.stderr)
        continue
    selected.add(name)
    for ref in find_refs(schemas[name]):
        if ref not in selected:
            queue.append(ref)

filtered = {k: v for k, v in schemas.items() if k in selected}
out = {
    "openapi": spec["openapi"],
    "info": spec["info"],
    "paths": {},
    "components": {"schemas": filtered},
}
json.dump(out, open("api/openapi.min.json", "w"), indent=2)
print(f"Selected {len(selected)} schemas: {sorted(selected)}")
