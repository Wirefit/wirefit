// Generator for wirefit-intro.pptx — run: node docs/deck-src.js (needs pptxgenjs).
// Palette "copper wire on slate": slate 22303C dominant, copper C96F3B accent,
// paper F4F1EC, muted 8C99A3. Motif: a thin wire with connector nodes.
const pptxgen = require("/tmp/deck/node_modules/pptxgenjs");

const SLATE = "22303C", SLATE_DK = "1A242E", COPPER = "C96F3B", PAPER = "F4F1EC",
  MUTED = "76838F", INK = "26313B", GREEN = "3E7C4F", RED = "B23A3A", AMBER = "C9952B",
  CARD = "FFFFFF", CODE_BG = "1E2832", CODE_FG = "D8DEE4";

const pres = new pptxgen();
pres.layout = "LAYOUT_16x9";
pres.author = "Dean Klopsch";
pres.title = "wirefit — your DTOs are already the contract";

const HEAD = "Trebuchet MS", BODY = "Calibri", MONO = "Consolas";

// wire motif: line with two connector nodes, color/cy configurable
function wire(s, y, color, x0 = 0.5, x1 = 9.5) {
  s.addShape(pres.shapes.LINE, { x: x0, y, w: x1 - x0, h: 0, line: { color, width: 1.5 } });
  s.addShape(pres.shapes.OVAL, { x: x0 - 0.05, y: y - 0.05, w: 0.1, h: 0.1, fill: { color } });
  s.addShape(pres.shapes.OVAL, { x: x1 - 0.05, y: y - 0.05, w: 0.1, h: 0.1, fill: { color } });
}

function title(s, txt, sub) {
  s.addText(txt, { x: 0.5, y: 0.32, w: 9, h: 0.62, fontFace: HEAD, fontSize: 30, bold: true, color: INK, margin: 0 });
  if (sub) s.addText(sub, { x: 0.5, y: 0.88, w: 9, h: 0.34, fontFace: BODY, fontSize: 13, italic: true, color: MUTED, margin: 0 });
}

function card(s, x, y, w, h, fill = CARD) {
  s.addShape(pres.shapes.RECTANGLE, {
    x, y, w, h, fill: { color: fill }, line: { color: "E4DFD6", width: 0.75 },
    shadow: { type: "outer", color: "000000", blur: 5, offset: 2, angle: 135, opacity: 0.12 },
  });
}

function dot(s, x, y, color, d = 0.16) {
  s.addShape(pres.shapes.OVAL, { x, y, w: d, h: d, fill: { color } });
}

// ---------------------------------------------------------------- slide 1: title
{
  const s = pres.addSlide();
  s.background = { color: SLATE_DK };
  // two wires meeting at a connector — the handshake
  s.addShape(pres.shapes.LINE, { x: 0.5, y: 1.75, w: 4.24, h: 0, line: { color: COPPER, width: 2 } });
  s.addShape(pres.shapes.LINE, { x: 5.26, y: 1.75, w: 4.24, h: 0, line: { color: "55636F", width: 2 } });
  s.addShape(pres.shapes.OVAL, { x: 4.74, y: 1.62, w: 0.26, h: 0.26, fill: { color: COPPER } });
  s.addShape(pres.shapes.OVAL, { x: 5.0, y: 1.62, w: 0.26, h: 0.26, fill: { color: "8C99A3" } });
  s.addText("wirefit", { x: 0.5, y: 2.05, w: 9, h: 1.0, align: "center", fontFace: HEAD, fontSize: 56, bold: true, color: PAPER });
  s.addText("Your DTOs are already the contract.", {
    x: 0.5, y: 3.05, w: 9, h: 0.45, align: "center", fontFace: BODY, fontSize: 19, color: "C9B8A6", italic: true,
  });
  s.addText("Polyglot contract checking — zero spec files, zero mock servers, git instead of a broker.", {
    x: 1.0, y: 3.65, w: 8, h: 0.4, align: "center", fontFace: BODY, fontSize: 13, color: "8C99A3",
  });
  s.addText("github.com/wirefit/wirefit", { x: 0.5, y: 4.85, w: 9, h: 0.3, align: "center", fontFace: MONO, fontSize: 12, color: "8C99A3" });
}

// ---------------------------------------------------------------- slide 2: the pain
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "The quiet way microservices break", "no compiler spans two repos");
  // left: the incident
  card(s, 0.5, 1.35, 4.3, 3.3);
  s.addText("Tuesday", { x: 0.75, y: 1.5, w: 3.9, h: 0.3, fontFace: HEAD, fontSize: 14, bold: true, color: COPPER, margin: 0 });
  s.addText([
    { text: "order-service removes a field nobody on the team remembers anyone using.", options: { breakLine: true } },
    { text: "", options: { breakLine: true } },
    { text: "Every test is green. The PR merges. The deploy succeeds.", options: { breakLine: true } },
    { text: "", options: { breakLine: true } },
    { text: "web-app still reads it. Production finds out first.", options: { bold: true } },
  ], { x: 0.75, y: 1.82, w: 3.8, h: 2.75, fontFace: BODY, fontSize: 14.5, color: INK, margin: 0 });
  // right: why the existing fix stalls
  card(s, 5.2, 1.35, 4.3, 3.3);
  s.addText("Why contract testing stalls", { x: 5.45, y: 1.5, w: 3.9, h: 0.3, fontFace: HEAD, fontSize: 14, bold: true, color: INK, margin: 0 });
  const stalls = [
    ["hand-written interaction tests", "per endpoint, per consumer, forever"],
    ["matchers, headers, provider states", "a second test suite to maintain"],
    ["a broker service to operate", "one more thing that pages you"],
  ];
  stalls.forEach((row, i) => {
    dot(s, 5.45, 1.98 + i * 0.78, RED, 0.13);
    s.addText(row[0], { x: 5.72, y: 1.86 + i * 0.78, w: 3.7, h: 0.3, fontFace: BODY, fontSize: 13, bold: true, color: INK, margin: 0 });
    s.addText(row[1], { x: 5.72, y: 2.13 + i * 0.78, w: 3.7, h: 0.28, fontFace: BODY, fontSize: 11.5, color: MUTED, margin: 0 });
  });
  s.addText("so most teams ship with no contract checks at all", {
    x: 5.45, y: 4.22, w: 3.9, h: 0.3, fontFace: BODY, fontSize: 12, italic: true, color: COPPER, margin: 0,
  });
  wire(s, 5.05, "D8CFC0");
}

// ---------------------------------------------------------------- slide 3: the bet
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "The bet: the contract already exists", "extract it from the code instead of writing it");
  // three flow boxes
  const steps = [
    ["1 · extract", "Provider DTOs are the contract.\nConsumer DTOs are a machine-readable declaration of what is actually read.", COPPER],
    ["2 · normalize", "One language-neutral IR.\nAbsence ≠ nullability. Canonical scalars: a Java long is not a JS number.", SLATE],
    ["3 · diff", "Direction-aware rules.\nWidening breaks readers; narrowing breaks senders. Breaking = blocked merge.", SLATE],
  ];
  steps.forEach((st, i) => {
    const x = 0.5 + i * 3.13;
    card(s, x, 1.45, 2.87, 2.15);
    s.addShape(pres.shapes.RECTANGLE, { x, y: 1.45, w: 2.87, h: 0.07, fill: { color: COPPER } });
    s.addText(st[0], { x: x + 0.2, y: 1.66, w: 2.5, h: 0.32, fontFace: HEAD, fontSize: 15, bold: true, color: INK, margin: 0 });
    s.addText(st[1], { x: x + 0.2, y: 2.02, w: 2.5, h: 1.45, fontFace: BODY, fontSize: 11.5, color: INK, margin: 0 });
    if (i < 2) s.addText("→", { x: x + 2.84, y: 2.25, w: 0.32, h: 0.4, fontFace: BODY, fontSize: 20, color: MUTED, align: "center", margin: 0 });
  });
  // payoff banner
  s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y: 3.95, w: 9, h: 1.05, fill: { color: SLATE } });
  s.addText([
    { text: "Because consumers declare what they read:  ", options: { color: PAPER } },
    { text: "removing a field nobody uses is NOT a breaking change.", options: { bold: true, color: "E8B98B" } },
  ], { x: 0.8, y: 4.05, w: 8.4, h: 0.5, fontFace: BODY, fontSize: 15, margin: 0, valign: "middle" });
  s.addText("The cleanup that schema registries and OpenAPI diffs can never allow.", {
    x: 0.8, y: 4.52, w: 8.4, h: 0.35, fontFace: BODY, fontSize: 11.5, italic: true, color: "C9B8A6", margin: 0,
  });
}

// ---------------------------------------------------------------- slide 4: onboarding
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "Onboarding is one file", "no build plugins · no spec files · no test code");
  // code card
  s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y: 1.4, w: 5.4, h: 3.7, fill: { color: CODE_BG } });
  s.addText("contracts.yaml", { x: 0.75, y: 1.5, w: 4.9, h: 0.28, fontFace: MONO, fontSize: 10.5, color: "7B8A98", margin: 0 });
  const code = [
    ["service: order-service", CODE_FG],
    ["provides:", CODE_FG],
    ["  - id: orders.get-order", "E8B98B"],
    ["    kind: rest", CODE_FG],
    ["    direction: response", CODE_FG],
    ["    dto: com.acme.api.OrderResponse", "9FD0A4"],
    ["consumes:", CODE_FG],
    ["  - id: billing.invoice-created", "E8B98B"],
    ["    provider: billing-service", CODE_FG],
    ["    dto: src/events/Invoice.ts#Invoice", "9FD0A4"],
  ];
  s.addText(code.map((c, i) => ({ text: c[0], options: { color: c[1], breakLine: i < code.length - 1 } })),
    { x: 0.75, y: 1.86, w: 4.9, h: 3.1, fontFace: MONO, fontSize: 12, margin: 0, lineSpacing: 19 });
  // right: stats + flow
  const stats = [["1", "YAML file per service"], ["0", "build-file changes"], ["< 5", "minutes to a working gate"]];
  stats.forEach((st, i) => {
    s.addText(st[0], { x: 6.3, y: 1.4 + i * 0.78, w: 1.1, h: 0.6, fontFace: HEAD, fontSize: 30, bold: true, color: COPPER, align: "right", margin: 0 });
    s.addText(st[1], { x: 7.5, y: 1.58 + i * 0.78, w: 2.1, h: 0.45, fontFace: BODY, fontSize: 12.5, color: INK, margin: 0 });
  });
  s.addShape(pres.shapes.RECTANGLE, { x: 6.3, y: 3.85, w: 3.2, h: 1.25, fill: { color: "EAE4D9" } });
  s.addText([
    { text: "wirefit extract", options: { fontFace: MONO, bold: true, breakLine: true } },
    { text: "asks Maven / Gradle / tsc / go / pydantic itself — the serializer's own machinery, never re-implemented.", options: { fontSize: 10.5, color: MUTED } },
  ], { x: 6.45, y: 3.95, w: 2.95, h: 1.1, fontFace: BODY, fontSize: 12, color: INK, margin: 0 });
}

// ---------------------------------------------------------------- slide 5: merge gate
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "Live: the merge gate", "demo.sh — three consumers, one provider, three PRs");
  // findings table mock
  card(s, 0.5, 1.4, 9, 2.6);
  const rows = [
    [RED, "BLOCKED", "remove customer_email", "field-removed · consumed by web-app, web-app-ts, web-app-zod"],
    [AMBER, "WARNING", "TS reads Java long as number", "scalar-lossy · unsafe beyond 2^53 — nobody else tells you this"],
    [COPPER, "ALLOWED", "same removal + recorded override", "justification + expiry mandatory, rendered in the PR comment"],
    [GREEN, "PASSES", "remove coupon_code", "no registered consumer reads it — safe cleanup, the payoff"],
  ];
  rows.forEach((r, i) => {
    const y = 1.62 + i * 0.58;
    dot(s, 0.8, y + 0.04, r[0], 0.18);
    s.addText(r[1], { x: 1.1, y, w: 1.05, h: 0.3, fontFace: MONO, fontSize: 11, bold: true, color: r[0], margin: 0 });
    s.addText(r[2], { x: 2.2, y, w: 3.1, h: 0.3, fontFace: BODY, fontSize: 12.5, bold: true, color: INK, margin: 0 });
    s.addText(r[3], { x: 5.4, y: y + 0.02, w: 3.85, h: 0.45, fontFace: BODY, fontSize: 10.5, color: MUTED, margin: 0 });
    if (i < 3) s.addShape(pres.shapes.LINE, { x: 0.75, y: y + 0.46, w: 8.5, h: 0, line: { color: "ECE6DB", width: 0.75 } });
  });
  s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y: 4.3, w: 9, h: 0.78, fill: { color: SLATE } });
  s.addText([
    { text: "Every breaking finding names its consumers.  ", options: { color: PAPER, bold: true } },
    { text: "The gate explains itself, in the terminal and in the PR.", options: { color: "9FB0BD" } },
  ], { x: 0.8, y: 4.3, w: 8.4, h: 0.78, fontFace: BODY, fontSize: 13.5, margin: 0, valign: "middle" });
}

// ---------------------------------------------------------------- slide 6: deploy gate
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "Live: what HEAD checks cannot see", "demo-deploy.sh — main is not what's running");
  const steps = [
    ["1", "consumer migrates off the field", "merged · published · NOT yet deployed", MUTED],
    ["2", "provider removes the field", "wirefit check: GREEN — correctly, vs main", GREEN],
    ["3", "can-i-deploy --env production", "BLOCKED: prod still runs the old consumer — names the field", RED],
    ["4", "consumer deploys · record-deploy", "provider unblocked · matrix all green", GREEN],
  ];
  steps.forEach((st, i) => {
    const y = 1.45 + i * 0.82;
    s.addShape(pres.shapes.OVAL, { x: 0.55, y: y + 0.05, w: 0.42, h: 0.42, fill: { color: i === 2 ? COPPER : SLATE } });
    s.addText(st[0], { x: 0.55, y: y + 0.05, w: 0.42, h: 0.42, align: "center", valign: "middle", fontFace: HEAD, fontSize: 15, bold: true, color: PAPER, margin: 0 });
    if (i < 3) s.addShape(pres.shapes.LINE, { x: 0.76, y: y + 0.5, w: 0, h: 0.36, line: { color: "C9C2B5", width: 1.5 } });
    s.addText(st[1], { x: 1.2, y: y - 0.02, w: 4.6, h: 0.32, fontFace: MONO, fontSize: 13, bold: true, color: INK, margin: 0 });
    s.addText(st[2], { x: 1.2, y: y + 0.27, w: 5.2, h: 0.3, fontFace: BODY, fontSize: 11.5, color: st[3], bold: st[3] !== MUTED, margin: 0 });
  });
  // right callout
  card(s, 6.7, 1.5, 2.8, 3.1, "EAE4D9");
  s.addText("the lockfile", { x: 6.95, y: 1.66, w: 2.3, h: 0.3, fontFace: HEAD, fontSize: 14, bold: true, color: COPPER, margin: 0 });
  s.addText([
    { text: "_envs/production.lock.json", options: { fontFace: MONO, fontSize: 10, breakLine: true } },
    { text: "", options: { breakLine: true } },
    { text: "record-deploy pins what runs where, by content hash.", options: { breakLine: true } },
    { text: "", options: { breakLine: true } },
    { text: "can-i-deploy answers against reality, not against main.", options: { bold: true } },
  ], { x: 6.95, y: 1.98, w: 2.35, h: 2.5, fontFace: BODY, fontSize: 12, color: INK, margin: 0 });
  s.addText("Still no broker. The deployed-version matrix is files in a git repo.", {
    x: 0.5, y: 5.0, w: 9, h: 0.3, fontFace: BODY, fontSize: 12, italic: true, color: MUTED, margin: 0,
  });
}

// ------------------------------------------------- slide 6b: the same thing with Pact (live)
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "The same scenario — the Pact way", "pact-examples/ · the order-service interaction, run live against a Pact Broker");

  // left: the machinery Pact requires
  card(s, 0.5, 1.4, 4.3, 3.0);
  s.addText("What Pact makes you run", { x: 0.75, y: 1.54, w: 3.85, h: 0.3, fontFace: HEAD, fontSize: 14, bold: true, color: INK, margin: 0 });
  const cost = [
    ["consumer: mock server + matchers", "web-app — a hand-written interaction test (JS)"],
    ["provider: replay + state handlers", "order-service — boots the service, one @State each (Java)"],
    ["a broker service to operate", "192.168.1.191:9292 — publish, verify, query"],
  ];
  cost.forEach((r, i) => {
    const y = 2.0 + i * 0.78;
    dot(s, 0.78, y + 0.02, RED, 0.13);
    s.addText(r[0], { x: 1.05, y: y - 0.06, w: 3.55, h: 0.3, fontFace: BODY, fontSize: 12.5, bold: true, color: INK, margin: 0 });
    s.addText(r[1], { x: 1.05, y: y + 0.21, w: 3.6, h: 0.4, fontFace: BODY, fontSize: 10.5, color: MUTED, margin: 0 });
  });

  // right: the live broker verdict
  card(s, 5.2, 1.4, 4.3, 3.0);
  s.addText("The broker's answer — live", { x: 5.45, y: 1.54, w: 3.85, h: 0.3, fontFace: HEAD, fontSize: 14, bold: true, color: INK, margin: 0 });
  const states = [
    [GREEN, "web-app", "verified — green on 1.0.0 & 2.0.0"],
    [RED, "mobile-app", "failed — 2.0.0 drops loyalty_tier"],
    [AMBER, "reporting-service", "pending — never verified"],
  ];
  states.forEach((r, i) => {
    const y = 1.98 + i * 0.42;
    dot(s, 5.46, y + 0.02, r[0], 0.14);
    s.addText(r[1], { x: 5.74, y: y - 0.04, w: 1.95, h: 0.3, fontFace: MONO, fontSize: 10, bold: true, color: INK, margin: 0 });
    s.addText(r[2], { x: 7.72, y: y - 0.02, w: 1.78, h: 0.3, fontFace: BODY, fontSize: 9, color: MUTED, margin: 0 });
  });
  s.addShape(pres.shapes.LINE, { x: 5.45, y: 3.32, w: 3.8, h: 0, line: { color: "ECE6DB", width: 0.75 } });
  s.addText("can-i-deploy order-service", { x: 5.45, y: 3.4, w: 3.85, h: 0.26, fontFace: BODY, fontSize: 10.5, italic: true, color: MUTED, margin: 0 });
  const cid = [
    ["2.0.0 → production", "NO", RED],
    ["2.0.0 → staging", "YES", GREEN],
    ["1.0.0 → production", "YES", GREEN],
  ];
  cid.forEach((r, i) => {
    const y = 3.68 + i * 0.25;
    s.addText(r[0], { x: 5.6, y, w: 2.7, h: 0.24, fontFace: MONO, fontSize: 10.5, color: INK, margin: 0 });
    s.addText(r[1], { x: 8.35, y, w: 0.95, h: 0.24, fontFace: MONO, fontSize: 10.5, bold: true, color: r[2], align: "right", margin: 0 });
  });

  // punchline banner
  s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y: 4.55, w: 9, h: 0.78, fill: { color: SLATE } });
  s.addText([
    { text: "Pact reaches this with a broker you run, publish to, and verify against.  ", options: { color: PAPER, bold: true } },
    { text: "wirefit prints the same deploy matrix from a git repo (slide 6) — same answer, zero servers.", options: { color: "9FB0BD" } },
  ], { x: 0.8, y: 4.55, w: 8.4, h: 0.78, fontFace: BODY, fontSize: 12.5, margin: 0, valign: "middle" });
}

// ---------------------------------------------------------------- slide 7: polyglot proof
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "One gate for every stack", "same logical type → byte-identical IR, enforced in CI");
  const langs = [
    ["Java / Kotlin", "Jackson introspection — naming strategies, @JsonIgnore, Optional, jakarta @Nonnull"],
    ["TypeScript + Zod", "compiler API · z.toJSONSchema — ?: vs | null kept distinct"],
    ["Go", "reflection generated inside your module — internal/ packages just work"],
    ["Python", "pydantic v2, external extractor on the frozen protocol v1 (~200 lines)"],
    ["proto3 · Avro · GraphQL", "schema files imported directly — the artifact IS the contract"],
    ["GraphQL queries", "persisted operations = the consumer's exact field usage"],
  ];
  langs.forEach((l, i) => {
    const x = 0.5 + (i % 2) * 4.62, y = 1.38 + Math.floor(i / 2) * 1.06;
    card(s, x, y, 4.38, 0.88);
    s.addShape(pres.shapes.RECTANGLE, { x, y, w: 0.07, h: 0.88, fill: { color: COPPER } });
    s.addText(l[0], { x: x + 0.25, y: y + 0.1, w: 3.95, h: 0.28, fontFace: HEAD, fontSize: 13, bold: true, color: INK, margin: 0 });
    s.addText(l[1], { x: x + 0.25, y: y + 0.39, w: 4.0, h: 0.42, fontFace: BODY, fontSize: 10.5, color: MUTED, margin: 0 });
  });
  s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y: 4.66, w: 9, h: 0.66, fill: { color: SLATE } });
  s.addText([
    { text: "14-case conformance corpus:  ", options: { color: PAPER, bold: true } },
    { text: "Java, TS, Go and Python produce hash-identical IR; the same kit certifies community extractors.", options: { color: "9FB0BD" } },
  ], { x: 0.8, y: 4.66, w: 8.4, h: 0.66, fontFace: BODY, fontSize: 12.5, margin: 0, valign: "middle" });
}

// ---------------------------------------------------------------- slide 8: honest gate
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "A gate teams don't turn off", "escape hatches that stay auditable");
  const rows = [
    ["overrides", "One finding, one interaction — with mandatory justification and expiry (max 180 days). Stale or expired overrides fail the build. Rendered in the PR comment: exceptions are never silent.", SLATE],
    ["org policy", "policy.yaml in the contracts repo: reclassify any rule org-wide, or mark it overridable: false. Governed by that repo's own code review.", SLATE],
    ["mirror check", "dto + schema on one interaction must agree byte-for-byte. Drift between code and schema file is unoverridable — exit 1, field-level report.", SLATE],
    ["never guess", "any, tuples, uint64, open inheritance, non-null Avro unions… are loud, named errors — not silently wrong schemas.", SLATE],
  ];
  rows.forEach((r, i) => {
    const y = 1.4 + i * 0.99;
    s.addShape(pres.shapes.RECTANGLE, { x: 0.5, y, w: 1.9, h: 0.78, fill: { color: r[2] } });
    s.addText(r[0], { x: 0.5, y, w: 1.9, h: 0.78, align: "center", valign: "middle", fontFace: HEAD, fontSize: 13.5, bold: true, color: PAPER, margin: 0 });
    s.addText(r[1], { x: 2.65, y, w: 6.8, h: 0.78, fontFace: BODY, fontSize: 11.5, color: INK, margin: 0, valign: "middle" });
  });
}

// ---------------------------------------------------------------- slide 9: architecture
{
  const s = pres.addSlide();
  s.background = { color: PAPER };
  title(s, "How it hangs together", "every box is a file or a process you already run");
  const W = 1.62, GAP = 0.225;
  const flow = [
    ["DTOs & schemas", "your code, untouched"],
    ["extractors", "per language · protocol v1"],
    ["canonical IR", "hashed · deterministic"],
    ["diff engine", "direction-aware rules"],
    ["contracts repo", "plain git — the broker"],
  ];
  flow.forEach((f, i) => {
    const x = 0.5 + i * (W + GAP);
    card(s, x, 1.55, W, 1.2);
    s.addText(f[0], { x: x + 0.06, y: 1.7, w: W - 0.12, h: 0.55, fontFace: HEAD, fontSize: 11.5, bold: true, color: INK, margin: 0, align: "center" });
    s.addText(f[1], { x: x + 0.06, y: 2.22, w: W - 0.12, h: 0.45, fontFace: BODY, fontSize: 9.5, color: MUTED, margin: 0, align: "center" });
    if (i < 4) s.addText("→", { x: x + W - 0.02, y: 2.0, w: GAP + 0.06, h: 0.4, fontFace: BODY, fontSize: 15, color: COPPER, align: "center", margin: 0 });
  });
  s.addText("the repo feeds three gates", {
    x: 0.5, y: 3.05, w: 9, h: 0.3, align: "center", fontFace: BODY, fontSize: 11, italic: true, color: MUTED, margin: 0,
  });
  const outs = [
    ["PR / MR comment", "check — blocks breaking merges", 0.5],
    ["deploy gate", "can-i-deploy — blocks unsafe rollouts", 3.55],
    ["matrix", "org-wide deployed compatibility", 6.6],
  ];
  outs.forEach((o) => {
    s.addShape(pres.shapes.LINE, { x: o[2] + 1.45, y: 3.4, w: 0, h: 0.25, line: { color: "C9C2B5", width: 1.25 } });
    card(s, o[2], 3.65, 2.9, 1.0, "EAE4D9");
    s.addText(o[0], { x: o[2] + 0.15, y: 3.78, w: 2.6, h: 0.3, fontFace: HEAD, fontSize: 12.5, bold: true, color: COPPER, margin: 0 });
    s.addText(o[1], { x: o[2] + 0.15, y: 4.08, w: 2.65, h: 0.5, fontFace: BODY, fontSize: 10.5, color: INK, margin: 0 });
  });
  s.addText("Deterministic by contract: identical inputs produce byte-identical IR — on every machine, in every language.", {
    x: 0.5, y: 4.95, w: 9, h: 0.3, fontFace: BODY, fontSize: 11.5, italic: true, color: MUTED, margin: 0, align: "center",
  });
}

// ---------------------------------------------------------------- slide 10: close
{
  const s = pres.addSlide();
  s.background = { color: SLATE_DK };
  wire(s, 1.0, COPPER, 0.5, 9.5);
  s.addText("git is the broker", { x: 0.5, y: 1.5, w: 9, h: 0.8, align: "center", fontFace: HEAD, fontSize: 38, bold: true, color: PAPER });
  const cols = [
    ["5 min", "per service:\none manifest, one CI include"],
    ["2 gates", "merge (vs main) and\ndeploy (vs production)"],
    ["0 servers", "contracts, lockfiles, policy —\nall files in a git repo"],
  ];
  cols.forEach((c, i) => {
    const x = 0.9 + i * 2.85;
    s.addText(c[0], { x, y: 2.6, w: 2.55, h: 0.6, align: "center", fontFace: HEAD, fontSize: 30, bold: true, color: COPPER });
    s.addText(c[1], { x, y: 3.2, w: 2.55, h: 0.7, align: "center", fontFace: BODY, fontSize: 11.5, color: "9FB0BD" });
  });
  s.addText("Both demos you just watched run in CI on every commit — the claims are executable.", {
    x: 0.5, y: 4.25, w: 9, h: 0.35, align: "center", fontFace: BODY, fontSize: 13, italic: true, color: "D8C7B2",
  });
  s.addText("github.com/wirefit/wirefit  ·  docs/USER-GUIDE.md  ·  Apache-2.0", {
    x: 0.5, y: 4.95, w: 9, h: 0.3, align: "center", fontFace: MONO, fontSize: 11, color: "55636F",
  });
}

pres.writeFile({ fileName: process.argv[2] || "wirefit-intro.pptx" }).then(() => console.log("deck written"));
