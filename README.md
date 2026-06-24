# wirefit — language-agnostic contract checking

> **Status: Phases 1–5 implemented (latest tag v0.3.0). Phase 3 core — Go extractor, public extractor protocol v1 + conformance kit,
> rule overrides with expiry (+ `override add` helper, org-level policy.yaml governance),
> GitLab CI component (beta), markdown PR/MR reports. Gradle + Maven paths both CI-covered;
> goreleaser snapshot verified; tsconfig path aliases confirmed working; 14-case corpus.
> Phase 4: env lockfiles, `record-deploy`, `can-i-deploy` (deployed-state gating with
> untracked/stale surfacing), `matrix` report — the deploy demo in
> [wirefit/examples](https://github.com/wirefit/examples) proves the
> HEAD-green-but-deploy-blocked scenario.
> Toolchain baselines (2026-06): Java 17 bytecode floor (CI on JDK 25), jakarta.annotation
> in all fixtures (javax matched too, by simple name), Jackson 2.22, TypeScript 6, Node 24 CI.
> Phase 5: schema-native importers (.proto / .avsc / GraphQL SDL + persisted-query
> projections), mirror check (code vs schema drift always fails), Python extractor via
> protocol v1 passing the corpus. Rust extractor deferred (PRD note).**
> Previously: Phase 2 —
> Java provider ↔ TS consumer checks work end-to-end with hash-identical IR proven by
> `conformance/run.sh`. Zod v4 schemas supported (runtime z.toJSONSchema). Remaining Phase 2: tsconfig path aliases.
> Previously: IR + diff engine, self-bootstrapping Java extractor
> with Maven/Gradle classpath auto-resolution (zero build-file changes), custom ObjectMapper
> hint, git-backed store, `init`/`extract`/`check`/`publish`, GitHub Action, OSS scaffolding,
> passing acceptance demo. Pre-release leftovers: project naming, Gradle-path CI coverage,
> goreleaser dry-run (see `../prds/phase-1-vertical-slice.md`).

`wirefit` catches breaking payload changes between services before merge. No pact files, no broker,
no mock servers: it extracts schemas from the DTOs your services already define, normalizes
them to a language-neutral IR, and runs a direction-aware semantic diff in CI.

## Install

wirefit is pure Go (`go 1.25`). Until the module is published, build from source:

```
go install ./cmd/wirefit          # builds to $(go env GOPATH)/bin, e.g. ~/go/bin
```

Put that directory on your PATH (add `export PATH="$HOME/go/bin:$PATH"` to your shell rc) and
`wirefit` is available everywhere. Or build to a fixed location instead:

```
go build -o /usr/local/bin/wirefit ./cmd/wirefit
```

(`go install github.com/wirefit/wirefit/cmd/wirefit@latest` will work once the module is
published.) IR extraction additionally needs a JDK 17+ for Java DTOs and Node for TS/Zod; the
Java extractor bootstraps its own pinned, checksum-verified Jackson jars on first run.

## Layout

```
cmd/wirefit/             CLI: validate, extract, check, publish, record-deploy, can-i-deploy, matrix, override, hash, diff, compat, extractor-test
internal/ir/        IR model, canonicalization (sorted-key JCS-style), content hashing, scalar fit table
internal/diff/      self-diff (before/after) + compat (producer vs consumer) engines, rule corpus in tests
internal/manifest/  contracts.yaml parsing + validation
internal/store/     git-backed contracts repo: publish, counterpart lookup, push with rebase-retry
internal/javatool/  embedded WirefitExtract source, pinned+checksummed jar bootstrap, maven/gradle classpath resolution
internal/tstool/    embedded extract.js (TS compiler API), pinned typescript npm bootstrap
conformance/        cross-language corpus: Java + TS + Go fixtures must produce hash-identical IR
internal/gotool/    Go extractor (generated reflection program inside the service module)
internal/extproto/  public extractor protocol v1 (docs/extractor-protocol.md)
internal/importer/  schema-native importers: .proto, .avsc, GraphQL SDL + operations
extractors/python/  pydantic v2 extractor (external, protocol v1) + corpus fixtures
internal/override/  rule overrides: (interaction,path,rule) downgrades with justification + expiry
ci/gitlab/          GitLab CI component (sticky MR note, beta)
extractors/java/    Java extractor fixtures, mapping docs, round-trip test
extractors/typescript/  TS mapping docs (canonical source in internal/tstool)
action/             GitHub composite action (PR gate + sticky comment)
examples/           maven-service + gradle-service build-system integration fixtures
```

The end-to-end consumer/provider demos live in a separate repo,
[wirefit/examples](https://github.com/wirefit/examples), which runs them against released wirefit.

## Try it

```
go test ./...                 # unit + rule corpus
go run ./cmd/wirefit diff --before testdata/example/before.json --after testdata/example/after.json \
  --direction response --consumers testdata/example/consumers.json
```

For the full acceptance scenario and the deploy-gating demo, see
[wirefit/examples](https://github.com/wirefit/examples) (`./run-demo.sh`, `./run-deploy-demo.sh`).

Exit codes: `0` ok/warnings · `1` breaking change · `2` config or input error.

The demo proves the Phase 1 acceptance criteria: a provider PR removing a **consumed** field
is blocked with the consumer named; removing an **unconsumed** field passes as safe.

## Java onboarding (the whole thing)

```
cd my-service
$EDITOR contracts.yaml                    # declare provides/consumes — one small file
                                          # (`wirefit init` scaffolding is planned)
wirefit extract                           # asks maven/gradle for the classpath itself
wirefit check --contracts-repo ../contracts
```

No build-file changes, no plugin to apply: `wirefit extract` interrogates the project's own
build tool (`mvn dependency:build-classpath` / an injected Gradle init script), and the
extractor bootstraps itself (embedded source, pinned + SHA-256-verified Jackson jars,
compiled once into the user cache). Custom Jackson config (Spring etc.): point
`settings.java-mapper` at any static `ObjectMapper` provider.

## Deploy gating & the matrix

The merge gate (`check`) compares against `main`. Deployment gating compares against what is
actually **running**, recorded per environment in the contracts repo:

```
wirefit publish       -f contracts.yaml --contracts-repo ../contracts                       # record the contract
wirefit record-deploy -f contracts.yaml --contracts-repo ../contracts --env production      # pin it as deployed
wirefit can-i-deploy  -f contracts.yaml --contracts-repo ../contracts --env production --ir .wirefit/ir
wirefit matrix        --contracts-repo ../contracts                                          # render the deployed matrix (md; --format json)
```

`can-i-deploy` blocks a rollout that would break a consumer still live in the target env, even
when the merge gate is green. `matrix` prints the org-wide deployed-compatibility table.
Stored IR (`contracts/**/*.ir.json`, `_blobs/`) is pretty-printed for readability — its content
hash is over the compact canonical form, so formatting never affects addressing or diffs. A
re-publish of an unchanged contract is an idempotent no-op.

## Notes / deliberate v0 choices

- Module path `github.com/wirefit/wirefit` is a placeholder pending the naming decision (PRD §8).
- **PRD 1.3 amendment:** Maven/Gradle *plugins* were dropped in favor of build-tool
  interrogation — same capability, zero build-file changes, and no Maven Central publishing
  on the adoption path. Gradle path implemented; CI coverage pending (Maven path is CI-tested).
- CLI uses stdlib `flag`, not cobra yet — swap when the command surface grows.
- Canonicalization is sorted-key compact JSON with literal-preserving numbers; full RFC 8785
  number formatting is deferred (IR numbers come from our own normalizer, so literals are stable).
- Enums are string-valued in IR v0 (matches Java enums / TS literal unions).
