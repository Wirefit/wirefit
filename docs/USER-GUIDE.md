# wirefit user guide

wirefit catches breaking payload changes between services **before merge and before
deploy** — without pact files, mock servers, or a broker to operate. It extracts schemas
from the DTOs your services already define, normalizes them into a language-neutral IR,
and runs a direction-aware semantic diff in CI. The store is a git repository.

This guide covers everything from first contact to org-wide rollout. For design rationale
see `../SPEC.md`; for the rule-by-rule reference see §7 below.

---

## 1. Concepts in five minutes

**Interaction.** One message shape flowing between services, identified by a dot-namespaced
id (`orders.get-order`). A REST request and its response are *two* interactions — they flow
in opposite directions and obey different rules.

**Direction.** Everything in wirefit is direction-aware:
- **P→C** (provider → consumer): responses, published events. Changes that *widen* what the
  producer may emit break consumers.
- **C→P** (consumer → provider): request bodies, consumed commands. Changes that *narrow*
  what the provider accepts break senders.

**Provider / consumer.** A service *provides* interactions (its DTOs are the contract) and
*consumes* interactions of others (its parsing DTOs are its **usage declaration**). This is
the heart of the model: because consumers register exactly what they read, removing a field
nobody reads is **not breaking** — the cleanup nobody else's tooling allows.

**IR.** A constrained JSON-Schema subset with extensions (`x-ct-scalar`, `x-ct-nullable`,
`x-ct-discriminator`, `x-ct-recursive`). Two key properties:
- *Absence ≠ nullability.* "May be missing" and "may be null" are tracked separately —
  most real-world contract bugs live in this distinction.
- *Canonical scalars.* `int32 int64 float32 float64 decimal string bytes uuid date datetime
  duration bool` — so a Java `long` read as a TS `number` produces a **lossiness warning**
  (unsafe beyond 2^53) instead of silently passing.

The IR is canonicalized and content-hashed; identical logical types produce byte-identical
IR across all extractors (enforced by the conformance corpus).

**Contracts repo.** A plain git repository where services publish their IR on merge to main.
No broker, no database, no service to run. PR checks read it; `publish` writes it.

**Finding classes.** 🔴 breaking (exit 1, blocks merge) · ⚠️ warning (visible, exit 0) ·
🟢 safe · ⚪ neutral.

---

## 2. Installation

```bash
go install github.com/wirefit/wirefit/cmd/wirefit@latest   # or grab a release binary
```

Toolchain expectations (only for the languages you actually extract):
| | needed |
|---|---|
| core CLI | nothing — static binary |
| Java extraction | the `wirefit-java` extractor + JDK 17+ on PATH/JAVA_HOME (bytecode floor 17; tested through 25), Maven or Gradle project |
| TypeScript / Zod | the `wirefit-ts` extractor + Node ≥ 22.6 + npm |
| Go | the service's own Go toolchain (built in) |
| Python | the `wirefit-py` extractor + Python 3 with pydantic v2 in the service environment |
| importers (.proto/.avsc/.graphql) | nothing — built in |

`wirefit` is the self-contained core CLI. Go and the schema importers stay built in.
Official non-core language support is delivered as plug-and-play extractor commands:
`wirefit-java`, `wirefit-ts`, and `wirefit-py`. Each command embeds the WireFit-owned
extractor code for that language and speaks the same public protocol as third-party
extractors. The language runtime and service dependencies still come from the service
environment: JDK/classpath for Java, Node/modules for TypeScript, and Python/Pydantic/imports
for Python.

First Java/TS/Python extraction may materialize embedded extractor code into your user cache
(`~/.cache/wirefit/`). Java also downloads pinned, SHA-256-verified Jackson jars for the
extractor side; TypeScript installs a pinned `typescript` package. Python does not create a
venv or install Pydantic, because it must run in the same environment as the service DTOs.

Trust boundary: run `wirefit extract` only against repositories you trust. Extraction may
execute the target project or its tooling: `wirefit-java` classpath resolution can run
`mvnw`/`gradlew`, Go extraction runs a generated `go run` inside the module, `wirefit-ts`
Zod extraction imports service modules, and external extractors are arbitrary commands
configured by the manifest.

---

## 3. Onboarding a service (the whole thing)

```bash
cd my-service
wirefit init                 # writes contracts.yaml + DTO candidate suggestions
$EDITOR contracts.yaml       # declare provides / consumes
wirefit extract              # DTOs → IR (external extractors resolve their own classpath)
wirefit check --contracts-repo ../contracts   # diff against published state
wirefit publish --contracts-repo ../contracts # on merge to main (CI does this)
```

No build-file changes. No plugin to apply. The manifest is the only configuration.

### The manifest: `contracts.yaml`

```yaml
service: order-service        # lowercase, [a-z0-9-]
schema-version: 1

provides:                     # what this service exposes
  - id: orders.get-order      # dot-namespaced; globally unique per provider
    kind: rest                # rest | event | rpc
    direction: response       # response | request | event
    dto: com.acme.orders.api.OrderResponse
  - id: orders.order-created
    kind: event
    direction: event
    schema: schemas/order-created.avsc   # schema-native artifact as the source (§5.6)
    # dto + schema together = mirror check: code and schema must agree (§5.7)

consumes:                     # what this service reads — its usage declaration
  - id: billing.invoice-created
    provider: billing-service
    dto: src/events/InvoiceCreated.ts#InvoiceCreated

extractors:                   # external extractors (protocol v1). Route DTO refs
  - match: ".ts"              # by file suffix, or "*" for suffix-less refs (java FQNs).
    command: "wirefit-ts"
  - match: "*"                # at most one "*" fallback; consulted after built-ins.
    command: "wirefit-java --build-tool maven"   # java config rides on the command
  # - match: ".py"
  #   command: "wirefit-py --python .venv/bin/python"

settings:                     # all optional
  unknown-fields: ignore      # reject if your deserializer is strict (flips rules, §7)
  graphql-schema: schema.graphql                      # SDL for operation-file projections
```

**DTO reference formats.** Routing is by registry order: the schema importers and Go are
built in; `.ts`/`.tsx` and bare Java FQNs are handled by the `wirefit-ts` and `wirefit-java`
extractors you route to under `extractors:`. Suffix rules match before Go; the single `*`
fallback matches after it (so `./pkg#Type` still reaches Go).

| format | extractor | routing |
|---|---|---|
| `com.acme.api.OrderResponse` | `wirefit-java` (Jackson introspection) | `extractors: {match: "*"}` |
| `src/views.ts#OrderView` | `wirefit-ts` (compiler API) | `extractors: {match: ".ts"}` |
| `src/schemas.ts#OrderSchema` | `wirefit-ts` Zod (runtime, if the export is a Zod schema) | `extractors: {match: ".ts"}` |
| `./internal/api#OrderResponse` | Go (reflection, generated in-module) | built in |
| `src/models.py#OrderView` | `wirefit-py` (Pydantic v2) | `extractors: {match: ".py"}` |
| `schemas/order.proto#Order` | proto importer | built in |
| `schemas/order-created.avsc` (`#Name` optional) | Avro importer | built in |
| `schema.graphql#Order` | GraphQL SDL importer | built in |
| `queries/getOrder.graphql` (no `#`) | GraphQL persisted-query projection | built in |

An unmatched reference fails with an actionable hint naming the `extractors:` entry to add.

---

## 4. Command reference

Exit codes everywhere: **0** ok/warnings · **1** breaking · **2** config/input error.

| command | purpose | key flags |
|---|---|---|
| `wirefit init` | scaffold a manifest + DTO suggestions | `--service`, `--scan`, `--force` |
| `wirefit validate` | validate the manifest (reports *every* problem) | `-f` |
| `wirefit extract` | DTOs/schemas → IR in `.wirefit/ir/` | `--project`, `--ir`, `-f` (java/classpath flags moved onto the `wirefit-java` command, §5.1) |
| `wirefit check` | candidate IR vs contracts repo (the PR gate) | `--contracts-repo`, `--ir`, `--overrides`, `--report file.md`, `--format text\|json` |
| `wirefit publish` | write IR + manifest copy to the contracts repo (merge to main) | `--contracts-repo`, `--no-commit` |
| `wirefit record-deploy` | pin published contracts as deployed in an env | `--env`, `--contracts-repo` |
| `wirefit can-i-deploy` | candidate vs what is **deployed** in an env | `--env`, `--ir`, `--from-env` + `--service` (promotion gate), `--stale-days`, `--report` |
| `wirefit matrix` | org-wide deployed compatibility table + promotion readiness | `--format term\|md\|html\|json`, `-o`, `--envs` |
| `wirefit override add` | append a justified, expiring override | `--justification` (required), `--days`, auto-fills from last check |
| `wirefit extractor-test` | conformance kit for third-party extractors | `--cases`, `--project`, `-- <command>` |
| `wirefit diff` / `compat` / `hash` | low-level IR plumbing | see `--help` |

### CI wiring

**GitHub** — one step, no classpath config needed:

```yaml
- uses: Wirefit/wirefit/actions/check@v0   # see actions/check/action.yml
  with:
    contracts-repo: acme/contracts
    token: ${{ secrets.CONTRACTS_REPO_TOKEN }}
```

PR → `check` + sticky markdown comment. Push to main → `check` + `publish`.

**GitLab** — include `ci/gitlab/wirefit.gitlab-ci.yml`, set `WIREFIT_CONTRACTS_REPO` and
`CONTRACTS_TOKEN`. MR note upsert + pipeline gate. (Status: beta.)

**Matrix as a Pages site**: publish the HTML deploy matrix so the whole org can see it:
GitHub via `Wirefit/wirefit/actions/pages@v0` (needs `pages: write` + `id-token: write`,
see `actions/pages/action.yml`); GitLab via the `pages` job that the include above already
provides (runs on the default branch, serves `wirefit matrix -o public/index.html`).

The full GitHub recipe, including the two repo settings that are not carried by the
workflow files (Pages enablement, the token secret), is in `CONTRACTS-REPO-SETUP.md`.

**Deploy pipelines** add two lines:

```bash
wirefit can-i-deploy --env production --contracts-repo contracts/   # gate
# ... deploy ...
wirefit record-deploy --env production --contracts-repo contracts/  # record reality
```

Promotion pipelines (staging → production) gate on what is *recorded on the source
stage* instead of a local build — no service checkout needed:

```bash
wirefit can-i-deploy --from-env staging --env production \
  --service order-service --contracts-repo contracts/
```

---

## 5. Per-language notes

Full mapping tables live next to each extractor — this is the short version.

### 5.1 Java (`wirefit-java`, `extractors/java/README.md`)
Routed via `extractors: {match: "*", command: "wirefit-java ..."}`. Jackson's own
introspection does the work: naming strategies, `@JsonIgnore`, `@JsonProperty(required)`,
`@JsonInclude` behave exactly as serialization does. Presence table: primitive → required
non-null · `Optional<T>` → optional · `@JsonInclude(NON_NULL)` → optional · `@Nonnull`
(jakarta/javax/JetBrains/Lombok, matched by simple name) → non-null · plain reference →
required **nullable**. Classpath comes from `mvn dependency:build-classpath` or an injected
Gradle init script — zero build-file changes.

Configuration rides on the `wirefit-java` command in the manifest, not on `wirefit extract`:
`--classpath` (explicit classpath, skips build-tool resolution), `--build-tool
auto|maven|gradle|none`, `--extractor-cp`, `--mapper <class-fqn>#<static-method>` (custom or
Spring ObjectMapper), `--java` (java binary). Example:
`command: "wirefit-java --build-tool gradle --mapper com.acme.Config#objectMapper"`.
(The old `settings.java-mapper` is deprecated: still parsed, warned as unused, pass
`--mapper` instead.)

### 5.2 TypeScript (`wirefit-ts`, `extractors/typescript/README.md`)
Routed via `extractors: {match: ".ts", command: "wirefit-ts"}`. The project's own tsconfig
drives the compiler (strict null checks required — without them nullability is unknowable
and extraction refuses). `field?:` → optional; `| null` → nullable; string-literal unions →
enums; discriminated unions → tagged unions with the discriminant lifted to the union level.
`number` is `float64` — reading a Java `long` as `number` warns (2^53). Path aliases resolve
automatically.

### 5.3 Zod (`wirefit-ts`)
Same extractor as §5.2. Point the manifest at an exported schema; it runtime-imports the module and converts
with the *service's own* zod v4 (`z.toJSONSchema`). `z.uuid()` / `z.iso.datetime()` give
richer scalars than the type system can. `.default()` fields: required on the provider
side, optional on the consumer side (io follows the manifest role). Because those semantics
differ per side, the same `.ts` ref used in *both* `provides` and `consumes` is rejected —
split it into two references.

### 5.4 Go (`extractors/golang/README.md`)
Reflection via a program generated *inside your module* (`.wirefit/gen/`, transient) — so
`internal/` packages just work. The `#TypeName` selector must be a Go identifier. Pointer
→ nullable; `,omitempty` → optional; embedded structs flatten. No enums/unions (language
limitation — use importers for schema-native payloads). `uint`/`uint64` and
`json.RawMessage` fail loudly.

### 5.5 Python (`wirefit-py`, `extractors/python/README.md`)
Routed via `extractors: {match: ".py", command: "wirefit-py ..."}`. The extractor uses
Pydantic v2's own `model_json_schema` output. Run it with the service Python environment so
application imports, plugins, aliases, and Pydantic behavior match production:
`command: "wirefit-py --python .venv/bin/python"`.

Pydantic v2 models and discriminated-union type aliases are supported. `int → int64`,
`X | None → nullable`, defaults → optional on the consumer side and required on the provider
side. Because those semantics differ per side, the same `.py` ref used in both `provides`
and `consumes` is rejected. Split it into two references.

### 5.6 Schema-native payloads (proto / Avro / GraphQL)
Where a schema artifact exists, it IS the source — wirefit imports it directly:
- **proto3**: proto3 JSON semantics; `optional`/message fields → optional; well-known types
  map to scalars; `oneof` members become optional fields (proto3 JSON has no discriminator
  on the wire); `uint64` rejected.
- **Avro**: every field required (Avro always encodes); `["null", T]` → nullable; logical
  types → uuid/decimal/date/datetime; non-null unions rejected (their JSON encoding is not
  plain JSON).
- **GraphQL SDL**: output fields → required (+nullable unless `!`); input fields → required
  iff non-null without default; unions/interfaces → tagged by `__typename`.
- **GraphQL persisted queries** — the gem: a consumer's `.graphql` operation file is its
  *exact* usage. Selections project against `settings.graphql-schema`. Field removals are
  then checked against what queries actually select.

### 5.7 Mirror check
An interaction with **both** `dto` and `schema` asserts they agree: extraction and import
must produce identical IR. Drift fails with a field-level report, exit 1, **no override
possible** — a schema file lying about the code is never acceptable.

---

## 6. Keeping the gate honest: overrides and org policy

**Overrides** (`wirefit-overrides.yaml`) let one specific finding through, on the record:

```yaml
overrides:
  - interaction: orders.get-order
    path: "$.customer_email"
    rule: field-removed
    downgrade-to: warning          # or safe
    justification: "JIRA-123 coordinated removal, consumers migrate this sprint"
    expires: "2026-07-15"          # mandatory, max 180 days out
```

Generate entries instead of hand-writing them: after a failing check,
`wirefit override add --justification "JIRA-123 ..."` auto-fills the coordinates when
exactly one breaking finding exists. Properties that keep this honest:
- binds to exact `(interaction, path, rule)` — refactors invalidate it loudly (stale
  override = exit 1, "remove it");
- expiry is mandatory and capped — temporary means temporary;
- the justification renders in the PR comment — exceptions are auditable, never silent.

**Org policy** (`policy.yaml` at the contracts-repo root, governed by that repo's reviews):

```yaml
rules:
  enum-value-added:
    class: warning          # reclassify org-wide
  field-removed:
    overridable: false      # nobody bypasses this rule, anywhere
```

Applied after the diff engine, before per-service overrides.

---

## 7. Compatibility rules reference

Direction is everything. Producer-side change, classified per direction:

| change | P→C (response/event) | C→P (request) |
|---|---|---|
| field removed | 🔴 if any consumer reads it · 🟢 if unconsumed | 🟢 (ignored) · 🔴 if provider rejects unknown fields |
| field added | 🟢 · 🔴 if a consumer rejects unknown fields | 🟢 optional · 🔴 required (unless all consumers already send it) |
| required → optional | 🔴 (presence was relied on) | 🟢 |
| optional → required | 🟢 | 🔴 (unless all consumers already send it) |
| nullable added | 🔴 | 🟢 |
| nullable removed | 🟢 | 🔴 |
| enum value added | 🔴 (unknown to consumers) | 🟢 |
| enum value removed | 🟢 | 🔴 (consumers may send it) |
| scalar change | by fit: widening 🔴 · narrowing 🟢 · lossy ⚠️ | mirrored |
| type kind change | 🔴 | 🔴 |
| union branch added | 🔴 | 🟢 |
| union branch removed | 🟢 | 🔴 |
| cross-language lossy pairing (int64↔float64, decimal↔float) | ⚠️ always | ⚠️ always |

Plus the consumer-usage refinement: any P→C breaking finding on a path **no registered
consumer reads** downgrades to 🟢 — provided consumers exist. With zero consumers
registered (cold start), breaking findings downgrade to ⚠️ warnings so the tool is
adoptable before coverage exists.

---

## 8. Deploy gating (the part HEAD checks can't do)

`check` proves main-vs-main. Production runs something older. The classic failure:

1. Consumer migrates off a field; merges. Main no longer reads it.
2. Provider removes the field. `check` is **green** — correctly, vs main.
3. Provider deploys. The **old** consumer build still running in prod breaks.

wirefit closes this with environment lockfiles in the contracts repo:

- `record-deploy --env production` after each rollout pins the deployed IR hashes
  (`_envs/production.lock.json`, content-addressed blobs in `_blobs/`).
- `can-i-deploy --env production` checks the candidate against **those** hashes. In the
  scenario above it blocks at step 3, naming the consumer and the exact field, until the
  consumer's deploy is recorded.
- Counterparts with no deploy record are checked against main and flagged *untracked* —
  never silently green. Records older than `--stale-days` (30) are flagged stale.
- `matrix` renders the whole org: env × consumer → provider/interaction with ✅/⚠️/🔴.
  Each side is labeled with its publish counter (`v4`, bumped per interaction whenever
  `publish` lands changed content, tracked in `contracts/<service>/versions.json`); the
  content hash stays in the HTML tooltip and the JSON output. Contracts published before
  the version log existed fall back to the hash label.

### Promotion readiness (stage N → stage N+1)

Declare the promotion order once, in the contracts repo:

```yaml
# _envs/pipeline.yaml
schema-version: 1
envs: [dev, staging, production]
```

With that file present (or `matrix --envs dev,staging,production` ad hoc), `matrix`
appends one *promotion* section per adjacent pair: for every service recorded on
stage N, would the version running **there** be compatible with what runs on stage
N+1? Services whose recorded hashes already match the next stage show as a single
*in sync* row. A blocked promotion does **not** fail `matrix` (exit codes stay about
the deployed state); the scriptable gate for one service's promotion is
`can-i-deploy --from-env <stage-N> --env <stage-N+1> --service <name>`.

Run the deploy demo (`run-deploy-demo.sh`) in [wirefit/examples](https://github.com/wirefit/examples) to watch the entire scenario.

---

## 9. Troubleshooting

| symptom | cause / fix |
|---|---|
| `manifest: ... must be dot-namespaced` | interaction ids need ≥ 2 segments: `domain.name` |
| `target/classes missing — compile first` | wirefit never builds your service; run `mvn compile` / your CI build step before `extract` |
| Java: field unexpectedly nullable | plain references are nullable by default; add `@Nonnull` or `@JsonInclude(NON_NULL)` (see §5.1 table) |
| TS: `tsconfig has neither strict nor strictNullChecks` | enable strict null checks — without them `\| null` is erased and nullability cannot be extracted |
| TS: `untyped value (any/unknown)` | intentional hard error: give it a concrete type or exclude it from the DTO |
| Zod: `z.toJSONSchema failed ... z.date()` | `z.date()` parses to a Date object — the wire shape is unclear; use `z.iso.datetime()` |
| Go: `cannot find module` in extraction | `extract` runs `go run` in your module — the module must build |
| `unknown corpus case` in extractor-test | case names must match the shipped corpus (`Scalars`, `Presence`, …) |
| `override ... matched no finding — remove it` | by design: the path/rule moved; the override is stale |
| `org policy forbids overriding rule X` | the contracts repo's `policy.yaml` wins; talk to its owners |
| `not published — run wirefit publish before recording deploys` | `record-deploy` pins *published* state; ensure main CI published first |
| can-i-deploy warns `untracked` | that counterpart never ran `record-deploy` in this env — adopt incrementally, the warning is the point |
| publish: `git push` fails repeatedly | someone else published concurrently; wirefit retries with pull-rebase ×3 — check repo permissions if it persists |

**Philosophy note:** wirefit *never guesses*. Anything it cannot represent faithfully —
`any`, tuples, open inheritance, `uint64`, non-null Avro unions, custom GraphQL scalars —
is a loud, named error rather than a silently wrong schema. If the gate is wrong about
something representable, that's a bug: file it with the DTO (the corpus grows from
exactly such reports).

---

## 10. FAQ

**Why is removing a field sometimes safe?** Because consumers declare what they read. If no
registered consumer's projection contains the path, nobody can break. That's the
consumer-driven payoff — and why consumer registration matters.

**Why did my check pass but can-i-deploy block?** `check` compares against main;
`can-i-deploy` against what's recorded as deployed. Different questions (§8).

**REST request and response — one interaction or two?** Two. Direction changes the rules.

**Can a provider and consumer share one DTO?** Within one manifest, no for Zod (io
semantics differ per side — split the schema). Across services: yes, that's the normal
case being checked.

**What about headers/status codes/auth?** Out of scope by design (SPEC C1). Body schemas
only — that's the part DTOs can prove.

**How do I write an extractor for language X?** Implement protocol v1 — an executable
reading JSON on stdin, writing IR on stdout (`docs/extractor-protocol.md`), then prove it
with `wirefit extractor-test`. The official extractors use the same protocol surface.
