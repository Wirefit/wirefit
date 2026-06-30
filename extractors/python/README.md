# wirefit Python extractor

Pydantic v2 models → wirefit IR, via pydantic's own `model_json_schema` (SPEC §6: the
ecosystem's machinery, never re-implemented). Built deliberately as an **external
extractor speaking protocol v1** (`docs/extractor-protocol.md`) — the dogfood proof that
community extractors need no wirefit source. Passes the conformance corpus:

```
wirefit extractor-test --cases extractors/python/fixtures/cases.yaml \
  --project extractors/python -- python3 extractors/python/wirefit_extract_py.py
```

## Wiring into a service

```yaml
# contracts.yaml
extractors:
  - match: ".py"
    command: "python3 path/to/wirefit_extract_py.py"   # or install on PATH
consumes:
  - id: orders.get-order
    provider: order-service
    dto: src/models.py#OrderView
```

Role mapping: consumes → `mode="validation"` (fields with defaults are optional);
provides → `mode="serialization"` (defaults are always emitted → required).

## Type mapping

`int → int64` (Python ints are arbitrary-precision; int64 is the honest interop ceiling —
beyond it use `Decimal`/`str`) · `float → float64` · `str → string` · `bool → bool` ·
`UUID → uuid` · `datetime → datetime` · `date → date` · `timedelta → duration` ·
`X | None → nullable` (distinct from optional-by-default, SPEC §7) ·
`Literal[...]`/str-enums → `enum` · `dict[str, T]` → open object carrying `T` ·
`list[T]` → array · nested models + self-references → recursion markers ·
`Field(discriminator=...)` unions → `oneOf` with lifted discriminator ·
union type aliases supported via `TypeAdapter` (point the spec at the alias name).

## Hard errors (fail loudly, never guess)

`Any`/untyped fields, tuples, non-string-keyed dicts, mixed dict+named fields,
non-string enums/consts, untagged object unions, pydantic v1 models.

## Not yet

- dataclasses / TypedDict (pydantic can wrap them — untested, report issues).
- Rust extractor (PRD 5.5) is deferred: no Rust toolchain in the dev environment yet;
  this extractor proves the same external-protocol path. `schemars` mapping is specced
  in the Phase 5 PRD.
