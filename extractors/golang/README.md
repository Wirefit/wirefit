# wirefit Go extractor

Reflection over struct tags, executed by a **generated program inside the service module**
(`.wirefit/gen/`, transient) — which is why `internal/` DTO packages just work, with the
service's own toolchain (PRD 3.1; canonical template in `internal/gotool/gotool.go`).

## Manifest reference format

```yaml
provides:
  - id: orders.get-order
    kind: rest
    direction: response
    dto: ./internal/api#OrderResponse   # ./package/path#TypeName
```

## Mapping

| Go declaration | IR |
|---|---|
| value field, no `omitempty` | required, non-nullable |
| `*T` | nullable (present, may be null) |
| `,omitempty` | optional (caveat: Go also drops zero values — `""`, `0`) |
| `string` / `bool` | `string` / `bool` |
| `int`, `int8/16/32`, `uint8/16` | `int32` |
| `int64`, `uint32`, `time.Duration` (nanos on the wire) | `int64` |
| `float32` / `float64` | `float32` / `float64` |
| `[]byte` | `bytes` |
| `time.Time` | `datetime` |
| `uuid.UUID` (github.com/google/uuid) | `uuid` |
| `[]T` | `array` |
| `map[string]T` | open object |
| embedded structs | flattened (encoding/json promotion) |
| recursion | `x-ct-recursive` |

## Hard errors (fail loudly, never guess)

`uint`/`uint64` (may exceed int64), `interface{}`/`any` fields, `json.RawMessage`,
non-string map keys, `,string` tag option (quoted numbers change the wire type),
duplicate names from embedded promotion, structs with no serializable fields.

**No enums, no unions:** Go has no language-level enum and no idiomatic tagged-union
encoding — those corpus cases are documented N/A for Go (see `conformance/run.sh`).
Schema-native payloads (proto/Avro) get importers in Phase 5.
