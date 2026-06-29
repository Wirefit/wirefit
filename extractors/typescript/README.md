# wirefit TypeScript extractor

Resolves declared DTO types through the project's **own TypeScript compiler** (the service's
`tsconfig.json` is honored; `strict`/`strictNullChecks` is required — without it `| null` is
erased and nullability cannot be tracked). Never re-implements type logic (SPEC §6).

## Invocation

Normal use is just `wirefit extract` — manifest `dto` entries of the form
`src/path/File.ts#TypeName` are routed to this extractor automatically (mixed Java/TS
manifests work). The extractor self-bootstraps: the script is embedded in the wirefit
binary and its pinned `typescript` dependency is npm-installed once into the user cache.
Canonical source: `internal/tstool/extract.js`.

## Type mapping

| TypeScript | IR |
|---|---|
| `string` | `string` |
| `number` | `float64` (pairs lossily with Java `long`/`BigDecimal` — SPEC F7 warning) |
| `bigint` | `int64` |
| `boolean` | `bool` |
| `Date` | `datetime` |
| `field?:` | optional (not in `required`) |
| `\| null` | `x-ct-nullable` — **distinct from optional** (SPEC §7) |
| string-literal unions / string enums | `enum` (sorted) |
| `T[]` / `Array<T>` | `array` |
| `Record<string, V>` / string index signature | open object carrying `V` (`additionalProperties: <V schema>`) |
| discriminated unions (shared literal discriminant) | `oneOf`; the discriminant property is lifted to the union level, matching the Java extractor |
| `Pick`/`Omit`/`Partial`/`Required`/intersections | resolved by the checker before mapping |
| recursion | `x-ct-recursive` marker |

## Hard errors (fail loudly, never guess)

`any`/`unknown`, tuples, numeric enums and numeric-literal types (IR enums are
string-valued in v1), function/method properties, untagged object unions, mixed
index-signature + named properties, tsconfig without strict null checks.

## Zod schemas (PRD 2.3)

A manifest `dto` may point at an **exported Zod schema** (`src/schemas.ts#OrderViewSchema`)
— detected automatically when the export is a value whose type is `Zod*`. The module is
runtime-imported (Node ≥ 22.6, type stripping) and converted with the **service's own
zod v4** via `z.toJSONSchema`, then normalized to IR:

- `z.uuid()` → `uuid`, `z.iso.datetime()` → `datetime`, `z.iso.date()` → `date` — richer
  scalars than the type system can express.
- io follows the manifest side: provides → `output`, consumes → `input` (so `.default()`
  fields are required on the provider side, optional on the consumer side).
- `z.int()` maps to `float64` like every JS number — mapping it to int64 would silence
  real >2^53 precision risk. Use `z.bigint()`-typed pipes if you mean true int64.
- Hard errors: zod v3 (no `toJSONSchema`), `z.date()`/`z.bigint()`/untyped transforms
  (unrepresentable on the wire — use `z.iso.datetime()` etc.), non-string enums.
- Keep schema files dependency-light: the Zod path executes the module.

## Path aliases

`tsconfig` `paths`/`baseUrl` aliases resolve automatically — the extractor builds the
program from the project's own parsed tsconfig, so whatever the service's compiler
resolves, the extractor resolves (verified by test). Monorepo *workspace* package
resolution (pnpm/yarn cross-package imports) remains untested — report issues.

## Not yet (tracked)

- io-ts / valibot detection (PRD P1: friendly "not supported" pointer).

The cross-language conformance corpus (`conformance/`) asserts that Java and TS fixtures
for the same logical type produce **hash-identical IR** — run `conformance/run.sh`.
