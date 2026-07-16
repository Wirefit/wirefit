package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
)

var matrixFixture = []matrixEdge{
	{Env: "prod", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order", Status: matrixStatusOK},
	{Env: "staging", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order",
		Status: matrixStatusWarning, Detail: "emitted int64, parsed as float64 | precision loss"},
	{Env: "staging", Consumer: "mobile", Provider: "order-service", Interaction: "orders.get-order",
		Status: matrixStatusIncompatible, Detail: "field removed <script>alert(1)</script>",
		Findings: []diff.Finding{
			{Class: diff.Breaking, Rule: "field-missing", Path: "$.items[]", Message: "field removed <script>alert(1)</script>"},
			{Class: diff.Warning, Rule: "type-narrowed", Path: "$.qty", Message: "int64 narrowed to int32"},
		},
		ConsumerBody: &ir.Schema{Type: "object", Required: []string{"items", "qty"}, Properties: map[string]*ir.Schema{
			"items": {Type: "array", Items: &ir.Schema{Type: "object", Properties: map[string]*ir.Schema{
				"sku": {Type: "string", Scalar: "string"}}}},
			"qty": {Type: "integer", Scalar: "int64"},
		}},
		ProviderBody: &ir.Schema{Type: "object", Required: []string{"qty"}, Properties: map[string]*ir.Schema{
			"qty": {Type: "integer", Scalar: "int32"},
		}},
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-01T12:00:00Z", RecordedBy: "alice", Hash: "abc123def456", Version: 3},
		ProviderRecord: &deployRecord{RecordedAt: "2026-04-28T09:30:00Z", RecordedBy: "bob", Hash: "0123456789ab", Version: 7}},
	// Version-less record: published before version logs existed, renders the hash.
	{Env: "staging", Consumer: "mobile", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusUntracked,
		Detail:         "provider has no deploy record in this env",
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-02T08:00:00Z", RecordedBy: "carol", Hash: "feedbeefcafe"}},
	{Env: "dev", Consumer: "web-app", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusError,
		Detail: "missing blob; re-publish + re-record"},
}

var promoFixture = []promoEdge{
	{From: "dev", To: "staging", Service: "order-service", Side: "provides", Counterpart: "web-app",
		Interaction: "orders.get-order", Status: matrixStatusIncompatible, Detail: "parser requires field <b>a</b>",
		Findings: []diff.Finding{{Class: diff.Breaking, Rule: "field-missing", Path: "$.a", Message: "parser requires field <b>a</b>"}},
		ConsumerBody: &ir.Schema{Type: "object", Required: []string{"a"}, Properties: map[string]*ir.Schema{
			"a": {Type: "string", Scalar: "string"},
		}},
		ProviderBody:   &ir.Schema{Type: "object"},
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-03T10:00:00Z", RecordedBy: "dana", Hash: "111111111111", Version: 4},
		ProviderRecord: &deployRecord{RecordedAt: "2026-05-02T09:00:00Z", RecordedBy: "erin", Hash: "222222222222", Version: 8}},
	{From: "dev", To: "staging", Service: "web-app", Status: matrixStatusOK, InSync: true,
		Detail: "in sync: the same contracts are already deployed in staging"},
	{From: "staging", To: "prod", Service: "order-service", Side: "consumes", Counterpart: "billing",
		Interaction: "invoices.get", Status: matrixStatusUntracked, Detail: "billing has no deploy record in prod",
		Findings: []diff.Finding{{Class: diff.Warning, Rule: "untracked-provider", Path: "$",
			Message: "billing has no deploy record in prod"}},
		ConsumerBody: &ir.Schema{Type: "object", Properties: map[string]*ir.Schema{
			"invoice": {Type: "string", Scalar: "string"},
		}},
		ProviderBody: &ir.Schema{Type: "object", Properties: map[string]*ir.Schema{
			"invoice": {Type: "string", Scalar: "string"},
		}},
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-04T11:00:00Z", RecordedBy: "frank", Hash: "333333333333", Version: 2}},
}

func TestRenderMatrixMD(t *testing.T) {
	out := string(renderMatrixMD(matrixFixture, nil))
	for _, want := range []string{
		"| prod | web-app |  | order-service / orders.get-order |  | ✅ ok |  |",
		"| staging | mobile | v3 | order-service / orders.get-order | v7 | 🔴 INCOMPATIBLE |",
		// Version-less records fall back to the hash.
		"| staging | mobile | feedbeefcafe | billing / invoices.get |  | ⚪ untracked |",
		`emitted int64, parsed as float64 \| precision loss`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("md output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, " float64 | precision") {
		t.Error("md output contains an unescaped pipe inside a cell")
	}
	if strings.Contains(out, "promotion readiness") {
		t.Error("md output must omit the promotion section without a pipeline")
	}
}

func TestRenderMatrixMDPromotions(t *testing.T) {
	out := string(renderMatrixMD(matrixFixture, promoFixture))
	for _, want := range []string{
		"## promotion readiness",
		"| dev → staging | order-service | provides orders.get-order ⇐ web-app | 🔴 INCOMPATIBLE |",
		"| dev → staging | web-app |  | ✅ ok | in sync: the same contracts are already deployed in staging |",
		"| staging → prod | order-service | consumes billing/invoices.get | ⚪ untracked |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("md output missing %q\n%s", want, out)
		}
	}
}

func TestRenderMatrixHTML(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, nil, nil))
	for _, want := range []string{
		`st-INCOMPATIBLE" title="the consumer would fail to read what the provider sends">INCOMPATIBLE</span>`,
		`<code>order-service/orders.get-order</code>`,
		"field removed &lt;script&gt;alert(1)&lt;/script&gt;",
		`<input type="checkbox" id="f-ok" checked>`,
		`<h2>staging<span class="gsum">1 INCOMPATIBLE · 1 warning · 1 untracked</span></h2>`,
		`<p class="verdict bad">2 failing edges in dev, staging</p>`,
		`<section class="group" id="env-staging" data-healthy="false">
<details open><summary><h2>staging`,
		`<section class="group" id="env-prod" data-healthy="true">
<details><summary><h2>prod`,
		`<button type="button" data-groups="expand">Expand all</button>`,
		`<button type="button" data-groups="collapse">Collapse all</button>`,
		`<button type="button" data-groups="collapse-healthy">Collapse healthy</button>`,
		`var selector = mode === "collapse-healthy"`,
		`if (target && target.matches(".group")) target.querySelector("details").open = true;`,
		`<tr class="row-error"><td>web-app</td>`,
		// The worst staging row opens its detail modal.
		`<tr class="row-INCOMPATIBLE" data-modal="d-env-staging-0"><td>mobile</td>`,
		`<th>consumer</th><th>version</th><th>provider / interaction</th><th>version</th>`,
		`<td class="ver"><code title="abc123def456 · recorded 2026-05-01T12:00:00Z by alice">v3</code></td>`,
		`<td class="ver"><code title="0123456789ab · recorded 2026-04-28T09:30:00Z by bob">v7</code></td>`,
		// A version-less record falls back to the hash in the cell.
		`<td class="ver"><code title="feedbeefcafe · recorded 2026-05-02T08:00:00Z by carol">feedbeefcafe</code></td>`,
		// Rows without a deploy record on a side leave that version blank.
		`<td class="ver"></td>`,
		`<dialog id="d-env-staging-0" aria-labelledby="d-env-staging-0-title">`,
		`<h3 id="d-env-staging-0-title">mobile → order-service/orders.get-order</h3>`,
		`<dl class="parties"><div><dt>consumer</dt><dd><code>mobile</code><span class="party-version">version <code>v3</code></span></dd></div><div><dt>provider</dt><dd><code>order-service</code><span class="party-version">version <code>v7</code></span></dd></div>`,
		`<button aria-label="close" autofocus>✕</button>`,
		`body.modal-open { overflow: hidden; }`,
		`document.body.classList.add("modal-open");`,
		`document.body.classList.remove("modal-open");`,
		`ev.clientX < box.left || ev.clientX > box.right`,
		`<code>field-missing</code>`,
		// Both bodies render side by side; the finding paths are highlighted
		// on every side where they resolve.
		`<h4>consumer projection</h4><pre>`,
		`<h4>provider schema</h4><pre>`,
		`<span class="hl st-warning">    &#34;qty&#34;: {</span>`,
		`<span class="hl st-INCOMPATIBLE">      &#34;items&#34;: {</span>`,
		// Descendants of a marked node inherit its highlight.
		`<span class="hl st-INCOMPATIBLE">          &#34;sku&#34;: {</span>`,
		// Unmarked lines render as plain text.
		"<pre>{\n",
		`consumer version <code>v3</code> · hash <code>abc123def456</code> · recorded 2026-05-01T12:00:00Z by alice`,
		`provider version <code>v7</code> · hash <code>0123456789ab</code> · recorded 2026-04-28T09:30:00Z by bob`,
		// The untracked edge has provenance for the recorded side only, and its
		// version-less record shows the hash once, without a hash suffix.
		`consumer version <code>feedbeefcafe</code> · recorded 2026-05-02T08:00:00Z by carol`,
		// Without a pipeline the strip still lists the envs, without arrows.
		`<nav class="pipeline">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<script>alert(1)") {
		t.Error("html output contains an unescaped <script> payload")
	}
	if strings.Contains(out, "data-exp") {
		t.Error("html output still contains inline expand rows")
	}
	if strings.Contains(out, `<span class="parrow`) {
		t.Error("html output must not render promotion arrows without a pipeline")
	}
	if strings.Contains(out, "promotion") {
		t.Error("html output must omit the promotion sections without a pipeline")
	}
	if empty := string(renderMatrixHTML(nil, nil, nil)); !strings.Contains(empty, "no deploy records") {
		t.Error("empty matrix html missing the no-deploy-records row")
	} else if strings.Contains(empty, `<nav class="pipeline">`) {
		t.Error("empty matrix html must not render a pipeline strip")
	}
}

func TestRenderMatrixHTMLPromotions(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, promoFixture, nil))
	for _, want := range []string{
		`<h2>promotion dev → staging<span class="gsum">1 INCOMPATIBLE · 1 ok</span></h2>`,
		`<h2>promotion staging → prod<span class="gsum">1 untracked</span></h2>`,
		`<section class="group" id="promo-dev---staging" data-healthy="false">
<details open><summary><h2>promotion dev → staging`,
		`<section class="group" id="promo-staging---prod" data-healthy="true">
<details><summary><h2>promotion staging → prod`,
		`<tr class="row-INCOMPATIBLE" data-modal="d-promo-dev---staging-0"><td>order-service</td><td><code>provides orders.get-order ⇐ web-app</code></td>`,
		"parser requires field &lt;b&gt;a&lt;/b&gt;",
		"· 3 promotion checks</p>",
		`<h3 id="d-promo-dev---staging-0-title">web-app → order-service/orders.get-order</h3>`,
		`<dl class="parties"><div><dt>consumer</dt><dd><code>web-app</code><span class="party-version">version <code>v4</code></span></dd></div><div><dt>provider</dt><dd><code>order-service</code><span class="party-version">version <code>v8</code></span></dd></div>`,
		`<h4>target consumer · staging</h4>`,
		`<h4>candidate provider · dev</h4>`,
		`provider version <code>v8</code> · hash <code>222222222222</code>`,
		// The enriched untracked row opens a modal with its findings.
		`<tr class="row-untracked" data-modal="d-promo-staging---prod-0"><td>order-service</td>`,
		`<dialog id="d-promo-staging---prod-0" aria-labelledby="d-promo-staging---prod-0-title">`,
		`<h3 id="d-promo-staging---prod-0-title">order-service → billing/invoices.get</h3>`,
		`<dl class="parties"><div><dt>consumer</dt><dd><code>order-service</code><span class="party-version">version <code>v2</code></span></dd></div><div><dt>provider</dt><dd><code>billing</code><span class="party-version">version unavailable</span></dd></div>`,
		`<h4>candidate consumer · staging</h4>`,
		`<h4>target provider · prod</h4>`,
		`consumer version <code>v2</code> · hash <code>333333333333</code>`,
		`<code>untracked-provider</code>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	// The in-sync rollup row has no check cell.
	if !strings.Contains(out, `<tr class="row-ok"><td>web-app</td><td></td>`) {
		t.Error("html output missing the empty-check in-sync row")
	}
	// Chip counts include promotion edges: 2 ok = 1 deployed + 1 in-sync.
	if !strings.Contains(out, ">2 ok</label>") {
		t.Error("chip counts must include promotion edges")
	}
	// Promotion rows within a pair render worst-status-first.
	if strings.Index(out, `row-INCOMPATIBLE"><td>order-service`) > strings.Index(out, `row-ok"><td>web-app</td><td></td>`) {
		t.Error("promotion rows are not worst-status-first")
	}
}

func TestRenderMatrixHTMLPipeline(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, promoFixture, []string{"dev", "staging", "qa"}))
	for _, want := range []string{
		`<a class="pnode" href="#env-dev">`,
		`<span class="badge st-error">dev</span>`,
		`<a class="pnode" href="#env-staging">`,
		`<span class="parrow pa-INCOMPATIBLE" title="promote dev → staging: 1 INCOMPATIBLE · 1 ok">→</span>`,
		`<span class="parrow pa-untracked" title="promote staging → qa: no promotion checks">→</span>`,
		// qa has no deploy records: a non-link node.
		`<span class="pnode"><span class="badge st-untracked">qa</span><span class="psum">no deploy records</span></span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	// Pipeline order first, then envs outside the pipeline.
	dev, staging := strings.Index(out, `href="#env-dev"`), strings.Index(out, `href="#env-staging"`)
	qa, prod := strings.Index(out, `>qa</span>`), strings.Index(out, `href="#env-prod"`)
	if !(dev < staging && staging < qa && qa < prod) {
		t.Errorf("strip order wrong: dev=%d staging=%d qa=%d prod=%d", dev, staging, qa, prod)
	}
	// Environment tables follow the configured pipeline too; environments
	// outside it come afterward in deterministic alphabetical order.
	dev = strings.Index(out, `id="env-dev"`)
	staging = strings.Index(out, `id="env-staging"`)
	prod = strings.Index(out, `id="env-prod"`)
	if !(dev < staging && staging < prod) {
		t.Errorf("environment table order wrong: dev=%d staging=%d prod=%d", dev, staging, prod)
	}
}

func TestPromoArrowInSyncTargetHealth(t *testing.T) {
	arrow := promoArrow([]promoEdge{{From: "dev", To: "staging", Service: "web-app",
		Status: matrixStatusIncompatible, InSync: true}}, "dev", "staging")
	if arrow.Status != matrixStatusIncompatible {
		t.Errorf("status = %s, want INCOMPATIBLE", arrow.Status)
	}
	for _, want := range []string{"no pending contract changes", "target compatibility: 1 INCOMPATIBLE"} {
		if !strings.Contains(arrow.Title, want) {
			t.Errorf("title = %q, want %q", arrow.Title, want)
		}
	}
}

func TestBodySide(t *testing.T) {
	s := &ir.Schema{Type: "object", Required: []string{"pet"}, Properties: map[string]*ir.Schema{
		"attrs": {Type: "object", AdditionalProperties: &ir.AdditionalProps{Value: &ir.Schema{Type: "string", Scalar: "string"}}},
		"pet": {Discriminator: "kind", OneOf: []*ir.Schema{
			{Type: "object", DiscriminatorValue: "cat", Properties: map[string]*ir.Schema{"lives": {Type: "integer", Scalar: "int32"}}},
			{Type: "object", DiscriminatorValue: "dog", Properties: map[string]*ir.Schema{"name": {Type: "string", Scalar: "string"}}},
		}},
	}}
	marks := bodyMarks([]diff.Finding{
		{Class: diff.Warning, Rule: "scalar-lossy", Path: "$.pet<dog>.name"},
		{Class: diff.Breaking, Rule: "map-value-open-vs-typed", Path: "$.attrs{}"},
	})
	lines := bodySide(s, marks)

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	joined := b.String()
	// The rendered form is the schema's own JSON, just re-indented.
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(joined)); err != nil {
		t.Fatalf("rendered body is not valid JSON: %v\n%s", err, joined)
	}
	want, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), want) {
		t.Errorf("rendered body does not match the schema's JSON:\n got %s\nwant %s", compact.Bytes(), want)
	}

	classOf := func(sub string) matrixStatus {
		for _, l := range lines {
			if strings.Contains(l.Text, sub) {
				return l.Class
			}
		}
		t.Fatalf("no line contains %q\n%s", sub, joined)
		return ""
	}
	if got := classOf(`"name": {`); got != matrixStatusWarning {
		t.Errorf("union branch field line class = %q, want warning", got)
	}
	if got := classOf(`"additionalProperties": {`); got != matrixStatusIncompatible {
		t.Errorf("map value line class = %q, want INCOMPATIBLE", got)
	}
	if got := classOf(`"lives": {`); got != "" {
		t.Errorf("unmarked line class = %q, want empty", got)
	}
}

func TestRenderMatrixDeterministic(t *testing.T) {
	pipeline := []string{"dev", "staging"}
	if !bytes.Equal(renderMatrixMD(matrixFixture, promoFixture), renderMatrixMD(matrixFixture, promoFixture)) {
		t.Error("md renderer is not deterministic")
	}
	if !bytes.Equal(renderMatrixHTML(matrixFixture, promoFixture, pipeline), renderMatrixHTML(matrixFixture, promoFixture, pipeline)) {
		t.Error("html renderer is not deterministic")
	}
}

func TestMatrixStatusHelpers(t *testing.T) {
	oldColor := colorEnabled
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = oldColor })

	tests := []struct {
		status matrixStatus
		glyph  string
		badge  string
	}{
		{matrixStatusOK, "✓", "✅"},
		{matrixStatusWarning, "⚠", "⚠️"},
		{matrixStatusIncompatible, "✗", "🔴"},
		{matrixStatusUntracked, "·", "⚪"},
		{matrixStatusError, "✗", "❗"},
		{matrixStatus("unknown"), "·", ""},
	}
	for _, tt := range tests {
		if got := matrixGlyph(tt.status); got != tt.glyph {
			t.Errorf("matrixGlyph(%q) = %q, want %q", tt.status, got, tt.glyph)
		}
		if got := matrixMDBadge(tt.status); got != tt.badge {
			t.Errorf("matrixMDBadge(%q) = %q, want %q", tt.status, got, tt.badge)
		}
	}
}
