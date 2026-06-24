# wirefit demo flow (14–17 minutes)

Presenter script: what to run, what to say, what the audience should feel at each beat.
Pairs with the slide deck (`wirefit-intro.pptx`). The demos live in the
[wirefit/examples](https://github.com/wirefit/examples) repo and run against an installed
`wirefit` — rehearse once so the extractor caches are warm and timings hold.

**Prep (before the audience arrives)**
```bash
# Build wirefit from the source checkout (go install @latest works only once published):
(cd wirefit && go build -o /usr/local/bin/wirefit ./cmd/wirefit)   # put wirefit on PATH
cd wirefit-examples
./run-demo.sh >/dev/null 2>&1 || true                       # warm jar + extractor caches
```
(If you skip the build, the demo scripts auto-build wirefit from `../wirefit` on first run,
provided `go` is installed — the first run is just slower.)

To browse the contracts repo live (git history, `_envs/*.lock.json`), point it at a real
checkout instead of the throwaway temp dir. Your clone of `wirefit-contracts` works and is
used as-is; `WIREFIT_RESET_REPO=1` resets it to a clean baseline each run so it's reproducible:
```bash
export WIREFIT_CONTRACTS_REPO=../wirefit-contracts WIREFIT_RESET_REPO=1
```
Heads-up: if that clone has a GitHub remote, every publish pushes to it (needs SSH + network,
and updates the real repo live — nice for showing GitHub's matrix CI). For a purely offline
rehearsal, `git -C ../wirefit-contracts remote remove origin` first.
Terminal: large font, dark theme, `cd wirefit-examples`. Slides up to slide 3 before going live.

---

## Beat 1 — The pain (slides 1–3, ~2 min, no terminal)

Say: *"Two services. One removes a field the other still reads. Nothing fails until
production. The existing fix — contract testing à la Pact — works, but you write
interaction tests by hand, maintain matchers, and run a broker. Most teams quietly don't.
wirefit's bet: your DTOs already ARE the contract. The consumer's parsing classes are a
machine-readable declaration of what it reads. Extract both sides, diff them, block the
merge. Setup is one YAML file."*

## Beat 2 — Onboarding is one file (slide 4, ~2 min)

```bash
cat order-service/contracts.yaml
cat web-app/contracts.yaml
```
Say: *"That's the entire integration. No build-plugin, no spec files, no test code. The
provider names what it exposes; each consumer names what it reads. Java here — same file
shape for TypeScript, Zod, Go, Python, or a .proto/.avsc/GraphQL schema."*

## Beat 3 — The merge gate, live (slide 5, ~4 min) ⭐

```bash
./run-demo.sh
```
Narrate while it scrolls (it's fast — pause output reading the highlights):
1. *"Provider publishes; three consumers register — plain TS interface, a Zod schema, a
   Java class. Watch the TS consumer's check..."* → point at the ⚠️ **scalar-lossy**
   warning: *"it reads a Java long as a JS number — unsafe past 2^53. No other tool tells
   you this."*
2. PR #1 → 🔴 **blocked**: *"removing `customer_email` names every consumer that reads it:
   web-app, web-app-ts, web-app-zod. The PR comment shows exactly this table."*
3. PR #1b → ⚠️ allowed: *"same removal WITH a recorded override — justification and expiry
   mandatory, rendered in the comment, gate stays up for everything else."*
4. PR #2 → 🟢 passes: *"removing `coupon_code` — nobody reads it. **Safe cleanup is the
   payoff**: schema registries call every removal breaking; wirefit knows better, because
   consumers declared their usage."*

## Beat 4 — The killer: deploy gating (slide 6, ~4 min) ⭐⭐

Say first: *"Everything so far compared against main. But production doesn't run main.
Here's the failure HEAD-checks can't see."*

```bash
./run-deploy-demo.sh
```
Narrate the four moments:
1. *"Consumer migrates off the field and merges. Provider removes the field. The PR check
   is **green** — correctly! Main no longer reads it."*
2. *"But `can-i-deploy --env production` **blocks**: the lockfile knows prod still runs the
   old consumer build, and names the exact field."* ← this is the moment; slow down.
3. *"Consumer's deploy pipeline runs `record-deploy`. Now the provider is unblocked."*
4. The matrix table: *"org-wide view of what's compatible with what, where. All of this is
   files in a git repo — there is still no server anywhere in this story."*

## Beat 4½ — The same thing, the Pact way (slide 6b, ~2 min) ⭐

Optional but high-impact if anyone says *"how is this different from Pact?"* — show the
Pact version of the very same scenario, running against a real broker.

```bash
# in the pact-examples repo, broker reachable on the LAN
cd pact-examples
node seed-broker.js          # publishes 3 consumers in 3 states + records deployments
open http://192.168.1.191:9292   # the broker UI: green / red / pending dashboard
```
Say: *"This is the identical order-service interaction, done with Pact. Three consumers:
web-app is **green**, mobile-app **fails** because it needs a field 2.0.0 dropped, reporting
is **pending** — never verified. And `can-i-deploy order-service 2.0.0 → production` is a
hard **NO**, because mobile-app is live in prod and 2.0.0 would break it."*

Then snap back: *"That's exactly the answer wirefit gave on the last slide — but Pact gets
it from a broker you stand up, publish to, and verify against. wirefit reads the same matrix
out of a git repo. Same safety; one of them is a server you operate."*

Point to `pact-examples/README.md` for the side-by-side: ~60 lines of consumer matcher DSL
+ a provider replay test + the broker, vs wirefit's one `contracts.yaml`.

## Beat 5 — One gate for everything (slide 8, ~2 min)

```bash
cat conformance/cases/Scalars/{Scalars.java,Scalars.ts,scalars.go}
./conformance/run.sh 2>/dev/null | tail -6
```
Say: *"Same logical type in Java, TypeScript, Go — byte-identical IR, same hash. 14-case
corpus, enforced in CI, and the same kit certifies third-party extractors — the Python one
is 200 lines speaking a frozen stdin/stdout protocol. Kafka/Avro, gRPC, GraphQL? Import
the schema file directly; GraphQL consumers register their persisted queries — their
exact field usage."*

If asked about code/schema drift: show the mirror check —
`dto` + `schema` on one interaction = both must agree, drift is unoverridable exit 1.

## Beat 6 — Close (slides 9–11, ~1 min)

Say: *"Five-minute onboarding per service. A git repo instead of a broker. Direction-aware
rules with an auditable escape hatch. And the two demos you just watched run in CI on
every commit — the README claims are executable."*

---

## Q&A ammunition

- **"What about headers/auth/status codes?"** Out of scope by design — body schemas only,
  that's what DTOs can prove. Pact remains the tool if you need full interaction recording.
- **"What if a consumer doesn't register?"** Cold start = warn-only; untracked consumers
  in can-i-deploy are checked against main and flagged. Never silently green.
- **"Monorepo?"** One manifest per service dir; `--project` + local `--contracts-repo` path.
- **"Trust the extraction?"** It's the serializer's own machinery (Jackson introspection,
  TS checker, pydantic) — and anything unrepresentable is a loud error, never a guess.
- **"Performance?"** Diff <2s; extraction is bounded by your build (classes must exist).
- **"Why not OpenAPI diff?"** Requires specs nobody maintains, and has no consumer-usage
  dimension — every removal is breaking forever.
