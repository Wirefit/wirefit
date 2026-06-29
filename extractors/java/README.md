# wirefit Java extractor

Emits wirefit IR for DTO classes using **Jackson's own introspection** — naming strategies,
`@JsonIgnore`, `@JsonProperty`, `@JsonInclude` behave exactly as the service serializes,
because it is the service's serializer doing the work (PRD 1.3 / SPEC §6).

## Invocation

Normal use is just `wirefit extract` — the CLI resolves the service classpath from the
project's own build tool (Maven: `dependency:build-classpath`; Gradle: injected init
script), bootstraps the extractor (embedded source + pinned, SHA-256-verified jars,
compiled once into the user cache), and runs:

```
java -cp <service classpath>:<extractor cache> [-Dwirefit.mapper=<fqn>#<method>] \
  io.wirefit.extract.WirefitExtract <dto-fqn>... > ir.json
```

The canonical source lives at `internal/javatool/WirefitExtract.java` (embedded into the Go
binary). Output: `{ "<fqn>": <IR schema>, ... }` on stdout. Exit 2 with a named type/path
on unsupported shapes (open inheritance, non-string map keys, `Object`/`JsonNode` fields).

Custom Jackson config: set `settings.java-mapper: com.acme.Config#objectMapper` in the
manifest (or `wirefit extract --mapper`) to use any static `ObjectMapper` provider — the
documented answer to Spring-configured mappers. `./test.sh` runs the fixture round-trip
used in CI.

## Presence / nullability mapping

| Java declaration | IR |
|---|---|
| primitive | required, non-nullable |
| `Optional<T>` | optional, non-nullable |
| `@JsonInclude(NON_NULL / NON_ABSENT / NON_EMPTY)` (field or class) | optional, non-nullable |
| `@JsonProperty(required = true)` | required (overrides) |
| `@Nonnull` / `@NotNull` / `@NonNull` (any package, matched by simple name) | non-nullable |
| any other reference type | required, **nullable** |

## Type mapping

`String/char → string`, `UUID → uuid`, `int/short → int32`, `long → int64`,
`float → float32`, `double → float64`, `BigDecimal/BigInteger → decimal`,
`boolean → bool`, `byte[] → bytes`, `LocalDate → date`,
`Instant/OffsetDateTime/ZonedDateTime/LocalDateTime/Date → datetime`, `Duration → duration`,
enums → string + sorted `enum` values (constant names; `@JsonValue` not yet honored),
collections/arrays → `array`, `Map<String, V>` → open object carrying `V` (`additionalProperties: <V schema>`),
`@JsonTypeInfo(use=NAME, include=PROPERTY)` + closed `@JsonSubTypes` → tagged union,
recursion → `x-ct-recursive` marker.

## Known v0 limits (fail loudly, never guess)

- Open inheritance (`@JsonTypeInfo` without `@JsonSubTypes`) — unsupported error.
- `@JsonValue` enums and custom serializers — not yet inspected.
- Spring `ObjectMapper` bean discovery (custom modules/config) — the blocking Phase 1 open
  question; current behavior is a default mapper + Jdk8Module.
