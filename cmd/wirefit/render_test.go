package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/diff"
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
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-01T12:00:00Z", RecordedBy: "alice", Hash: "abc123def456"},
		ProviderRecord: &deployRecord{RecordedAt: "2026-04-28T09:30:00Z", RecordedBy: "bob", Hash: "0123456789ab"}},
	{Env: "staging", Consumer: "mobile", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusUntracked,
		Detail:         "provider has no deploy record in this env",
		ConsumerRecord: &deployRecord{RecordedAt: "2026-05-02T08:00:00Z", RecordedBy: "carol", Hash: "feedbeefcafe"}},
	{Env: "dev", Consumer: "web-app", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusError,
		Detail: "missing blob; re-publish + re-record"},
}

var promoFixture = []promoEdge{
	{From: "dev", To: "staging", Service: "order-service", Side: "provides", Counterpart: "web-app",
		Interaction: "orders.get-order", Status: matrixStatusIncompatible, Detail: "parser requires field <b>a</b>"},
	{From: "dev", To: "staging", Service: "web-app", Status: matrixStatusOK,
		Detail: "in sync: the same contracts are already deployed in staging"},
	{From: "staging", To: "prod", Service: "order-service", Side: "consumes", Counterpart: "billing",
		Interaction: "invoices.get", Status: matrixStatusUntracked, Detail: "billing has no deploy record in prod",
		Findings: []diff.Finding{{Class: diff.Warning, Rule: "untracked-provider", Path: "$",
			Message: "billing has no deploy record in prod"}}},
}

func TestRenderMatrixMD(t *testing.T) {
	out := string(renderMatrixMD(matrixFixture, nil))
	for _, want := range []string{
		"| prod | web-app | order-service / orders.get-order | ✅ ok |  |",
		"| staging | mobile | order-service / orders.get-order | 🔴 INCOMPATIBLE |",
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
		`<section id="env-staging">`,
		`<tr class="row-error"><td>web-app</td>`,
		// The worst staging row expands to its findings and provenance.
		`<tr class="row-INCOMPATIBLE" data-exp><td>mobile</td>`,
		`<th>consumer</th><th>version</th><th>provider / interaction</th><th>version</th>`,
		`<td class="ver"><code title="recorded 2026-05-01T12:00:00Z by alice">abc123def456</code></td>`,
		`<td class="ver"><code title="recorded 2026-04-28T09:30:00Z by bob">0123456789ab</code></td>`,
		// Rows without a deploy record on a side leave that version blank.
		`<td class="ver"></td>`,
		`<tr class="exp row-INCOMPATIBLE" hidden>`,
		`<code>field-missing</code>`,
		`consumer version <code>abc123def456</code> · recorded 2026-05-01T12:00:00Z by alice`,
		`provider version <code>0123456789ab</code> · recorded 2026-04-28T09:30:00Z by bob`,
		// The untracked edge has provenance for the recorded side only.
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
		`<tr class="row-INCOMPATIBLE"><td>order-service</td><td><code>provides orders.get-order ⇐ web-app</code></td>`,
		"parser requires field &lt;b&gt;a&lt;/b&gt;",
		"· 3 promotion checks</p>",
		// The enriched untracked row expands to its findings.
		`<tr class="row-untracked" data-exp><td>order-service</td>`,
		`<tr class="exp row-untracked" hidden><td colspan="4"><table class="findings">`,
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
