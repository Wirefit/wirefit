# wirefit — launch checklist

All five roadmap phases are implemented and verified (v0.2.0 + toolchain-baseline commit).
This is the path from "done on disk" to "public project". Order matters in §1–§3;
the rest can be parallelized.

## 1. Claim the names (do this FIRST — availability was verified 2026-06-09 and is point-in-time)

- [x] GitHub org `wirefit` + repo `wirefit/wirefit`
- [x] npm: publish a placeholder `wirefit` package (and optionally the `@wirefit` scope)
- [x] PyPI: register `wirefit` (placeholder, for the future packaged Python extractor)
- [x] crates.io: register `wirefit` (cheap insurance for the Rust extractor)
- [ ] Domain: `wirefit.dev` (was unregistered at last check)
- [ ] Optional: Homebrew tap repo `wirefit/homebrew-tap` (then flip `skip_upload` in `.goreleaser.yaml`)

## 2. First push + CI green in the real world

- [x] `git remote add origin git@github.com:wirefit/wirefit.git && git push -u origin master --tags`
- [ ] Branch protection on `master`: require the `ci` workflow checks
- [x] Watch the first CI run — it exercises paths the sandbox could not:
  - [x] JDK 25 (Temurin) — local verification was on 17; the floor (`--release 17`) protects compatibility, but confirm
  - [x] Node 24 — local was 22
  - [x] Gradle job via `setup-gradle` (local run used 8.14.4; Gradle 9.x on runners is expected to work — confirm)
  - [ ] Restore executable bits if CI complains: `git update-index --chmod=+x` on the `.sh` files (the dev mount drops them on edit; git modes were committed correctly, so this is likely a non-issue)
- [ ] Verify the conformance + extractor-test + both demo jobs are green

## 3. First real release

- [ ] Decide release version: recommend `v0.3.0` (v0.1/v0.2 exist as local milestones; first public tag should be fresh)
- [ ] Pre-tag review checklist (release-blocking, per PRD):
  - [ ] Extractor protocol v1 freeze review — once public, it's additive-only forever
  - [ ] IR keyword set review (SPEC §7) — same reasoning
  - [ ] `go.mod` module path matches the real repo (`github.com/wirefit/wirefit` ✓)
- [ ] `git tag -a v0.3.0 && git push --tags` → release workflow runs goreleaser (snapshot was verified locally: 6 platforms, checksums)
- [ ] Un-draft the GitHub release, sanity-install on one machine: `go install github.com/wirefit/wirefit/cmd/wirefit@v0.3.0`
- [ ] Action consumers: tag `action/` usage as `wirefit/wirefit/action@v0.3.0`; replace the `go install @latest` step in `action.yml` with the pinned release binary + checksum (tracked TODO in the file)

## 4. Live-fire tests that need real infrastructure

- [ ] GitLab component on a real runner (status: implemented, beta, never executed) — set up the demo project + `CONTRACTS_TOKEN`, verify MR note upsert
- [ ] GitHub Action end-to-end on a real PR: sticky comment, publish-on-main, contracts-repo auth recipe (deploy key vs PAT — document the working one)
- [ ] A real two-repo walkthrough following only the README (the "<15 min stranger test", PRD Phase 1 metric) — recruit one person who hasn't seen the project

## 5. Docs polish before announcing

- [ ] README top: 30-second pitch + the killer demo GIF/asciinema (`run-demo.sh` then `run-deploy-demo.sh` from the wirefit/examples repo)
- [ ] Add SECURITY.md (binary runs in CI: report channel matters) + issue templates (bug / wrong-classification / new-extractor)
- [ ] Rule reference page: generate the breaking/safe table per direction from the corpus (PRD open question — even a manual first version)
- [ ] Comparison table vs Pact / Specmatic / buf / oasdiff / registries (exists in SPEC §1 — surface it in README)
- [ ] CHANGELOG.md seeded from the tag messages

## 6. Announce

- [ ] Show HN draft: lead with the deploy-gate demo (HEAD-green-but-prod-blocked) — it's the differentiator pact users feel
- [ ] r/java, r/golang, r/typescript posts angled per audience (Java: zero build-file changes; TS: Zod + persisted queries; Go: internal/ packages just work)
- [ ] Pact community spaces: position as complement-or-alternative honestly (schema-only, no interaction tests — different tradeoff, link SPEC §1)

## 7. Post-launch backlog (PRD P1s, in rough value order)

- [ ] Schema-registry import (Confluent/Apicurio read-only) — biggest Kafka-org unlock
- [ ] `--strict-env` flag + env-name registry (`_envs/envs.yaml`) — Phase 4 open questions
- [ ] GraphQL deprecation → "safe to remove" report (the consumer-usage payoff demo)
- [ ] 100-service × 3-env synthetic benchmark repo (Phase 4 perf metric + long-term fixture)
- [ ] Rust extractor (`schemars`, protocol v1 — spec in Phase 5 PRD; Python is the template)
- [ ] Flagship mixed-org demo: REST + Kafka/Avro + GraphQL gateway in one contracts repo
- [ ] proto2 (degraded), JSON-Schema-file importer, io-ts/valibot friendly errors
- [ ] SLSA provenance / artifact signing in goreleaser (table stakes for a CI-resident binary)

## Known limitations to disclose in README (honesty beats surprise)

- Overrides bind to exact (interaction, path, rule); refactors invalidate them loudly (by design)
- Go: no enums/unions (language limitation, documented N/A in corpus)
- proto: oneof → optional fields (proto3 JSON has no wire discriminator); field numbers not checked
- Avro: non-null unions rejected; GraphQL: custom scalars beyond DateTime/Date/UUID rejected; inline fragments in operations rejected
- `record-deploy` records the published state of the checked-out commit (rollback = record from the rolled-back commit's pipeline; `--ir-hash` flag deferred)
- Lockfile write contention bounded by deploy frequency — a git repo is the store by design
