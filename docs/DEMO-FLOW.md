# wirefit demo flow (12–15 minutes)

Presenter script: what to run, what to say, what the audience should feel at each beat.
Pairs with the slide deck (`wirefit-intro.pptx`). Everything runs offline from the repo —
rehearse once so the extractor caches are warm and timings hold.

**Prep (before the audience arrives)**
```bash
cd wirefit
go build -o /tmp/wirefit ./cmd/wirefit          # warm build cache
./extractors/java/fetch-jars.sh                 # warm jar cache
./examples/demo.sh >/dev/null 2>&1 || true      # warm extractor caches
```
Terminal: large font, dark theme, `cd wirefit`. Slides up to slide 3 before going live.

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
cat examples/order-service/contracts.yaml
cat examples/web-app/contracts.yaml
```
Say: *"That's the entire integration. No build-plugin, no spec files, no test code. The
provider names what it exposes; each consumer names what it reads. Java here — same file
shape for TypeScript, Zod, Go, Python, or a .proto/.avsc/GraphQL schema."*

## Beat 3 — The merge gate, live (slide 5, ~4 min) ⭐

```bash
./examples/demo.sh
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
./examples/demo-deploy.sh
```
Narrate the four moments:
1. *"Consumer migrates off the field and merges. Provider removes the field. The PR check
   is **green** — correctly! Main no longer reads it."*
2. *"But `can-i-deploy --env production` **blocks**: the lockfile knows prod still runs the
   old consumer build, and names the exact field."* ← this is the moment; slow down.
3. *"Consumer's deploy pipeline runs `record-deploy`. Now the provider is unblocked."*
4. The matrix table: *"org-wide view of what's compatible with what, where. All of this is
   files in a git repo — there is still no server anywhere in this story."*

## Beat 5 — One gate for everything (slide 7, ~2 min)

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

## Beat 6 — Close (slides 8–10, ~1 min)

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
