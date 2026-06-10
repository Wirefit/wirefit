# wirefit extractor protocol v1

**Status: frozen.** Evolution is additive only; a breaking change requires `schemaVersion: 2`.

An extractor is **any executable** that turns DTO references into wirefit IR. This is the
community surface (PRD 3.2): a PHP/C#/Kotlin/Python extractor is an independent package that
implements this protocol — no wirefit source required.

## Wire format

Request on **stdin**, response on **stdout**, both JSON. Diagnostics go to stderr.
Exit non-zero (and/or set `error`) on failure.

```jsonc
// stdin
{
  "schemaVersion": 1,
  "projectDir": "/abs/path/to/service",
  "specs": [
    { "ref": "src/models.py#OrderView", "role": "consumed" },
    { "ref": "src/models.py#InvoiceOut", "role": "provided" }
  ]
}
```

```jsonc
// stdout
{
  "schemaVersion": 1,
  "schemas": {
    "src/models.py#OrderView":  { "type": "object", "properties": { /* wirefit IR */ } },
    "src/models.py#InvoiceOut": { "type": "object", "properties": { /* wirefit IR */ } }
  }
}
// or: { "schemaVersion": 1, "schemas": {}, "error": "unsupported shape at src/models.py#X.field: ..." }
```

## Contract

- `ref` is the manifest `dto` string verbatim. Its internal format (`file#Type`, FQN, …)
  is the extractor's own convention; wirefit routes by the manifest `extractors:` matcher.
- `role` is `provided` (service emits this shape) or `consumed` (service parses it).
  Honor it wherever your source distinguishes input/output semantics (defaults, transforms).
- Emitted documents must be valid wirefit IR (SPEC §7): the JSON Schema subset with
  `x-ct-scalar`, `x-ct-nullable`, `x-ct-recursive`, `x-ct-discriminator(-value)`.
  wirefit re-validates and canonicalizes everything — but invalid IR fails the run.
- **Determinism (NF3):** identical inputs must produce identical output. No timestamps,
  no random ordering.
- **No data leakage (NF5):** structure only — never example values, constants, or secrets.
- **Fail loudly, never guess:** an unrepresentable construct is an `error` naming the type,
  file and field — not a silently wrong schema.

## Wiring into a service

```yaml
# contracts.yaml
extractors:
  - match: ".py"                  # file-suffix match on the dto reference
    command: "wirefit-extract-py" # resolved via PATH, executed in the service repo
consumes:
  - id: orders.get-order
    provider: order-service
    dto: src/models.py#OrderView
```

## Conformance

Run the corpus against your extractor before publishing:

```
wirefit extractor-test --cases cases.yaml --project ./fixtures -- wirefit-extract-py
```

`cases.yaml` maps corpus case names to your fixture specs:

```yaml
cases:
  - name: Scalars
    spec: fixtures/scalars.py#Scalars
    role: consumed
  - name: Presence
    spec: fixtures/presence.py#Presence
    role: consumed
```

Your fixtures must express the same logical types as the corpus
(`conformance/cases/` — Java and TypeScript fixtures double as reference
implementations). The kit compares canonical IR hashes; a pass means your
extractor agrees byte-for-byte with every other wirefit extractor.
