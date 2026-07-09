# wirefit Python extractor

Pydantic v2 models → wirefit IR, via pydantic's own `model_json_schema` (SPEC §6: the
ecosystem's machinery, never re-implemented). `wirefit-py` is the official external
extractor executable speaking protocol v1 (`docs/extractor-protocol.md`). It embeds the
WireFit-owned Python extractor source, then runs it with the service's Python environment.
Passes the conformance corpus:

```
wirefit extractor-test --cases extractors/python/fixtures/cases.yaml \
  --project extractors/python -- wirefit-py --python .venv/bin/python
```

## Wiring into a service

```yaml
# contracts.yaml
extractors:
  - match: ".py"
    command: "wirefit-py --python .venv/bin/python"
consumes:
  - id: orders.get-order
    provider: order-service
    dto: src/models.py#OrderView
```

Role mapping: consumes → `mode="validation"` (fields with defaults are optional);
provides → `mode="serialization"` (defaults are always emitted → required).
Python, Pydantic v2, and application imports must be available from the selected Python
environment. The extractor does not create a venv or install dependencies.

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
