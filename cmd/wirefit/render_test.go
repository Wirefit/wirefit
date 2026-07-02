package main

import (
	"bytes"
	"strings"
	"testing"
)

var matrixFixture = []matrixEdge{
	{Env: "prod", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order", Status: matrixStatusOK},
	{Env: "staging", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order",
		Status: matrixStatusWarning, Detail: "emitted int64, parsed as float64 | precision loss"},
	{Env: "staging", Consumer: "mobile", Provider: "order-service", Interaction: "orders.get-order",
		Status: matrixStatusIncompatible, Detail: "field removed <script>alert(1)</script>"},
	{Env: "staging", Consumer: "mobile", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusUntracked,
		Detail: "provider has no deploy record in this env"},
	{Env: "dev", Consumer: "web-app", Provider: "billing", Interaction: "invoices.get", Status: matrixStatusError,
		Detail: "missing blob; re-publish + re-record"},
}

func TestRenderMatrixMD(t *testing.T) {
	out := string(renderMatrixMD(matrixFixture))
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
}

func TestRenderMatrixHTML(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture))
	for _, want := range []string{
		`<span class="badge st-INCOMPATIBLE">INCOMPATIBLE</span>`,
		`<code>order-service/orders.get-order</code>`,
		"field removed &lt;script&gt;alert(1)&lt;/script&gt;",
		`<input type="checkbox" id="f-ok" checked>`,
		`<h2>staging<span class="gsum">1 INCOMPATIBLE · 1 warning · 1 untracked</span></h2>`,
		`<p class="verdict bad">2 failing edges in dev, staging</p>`,
		`<tr class="row-error">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<script>") {
		t.Error("html output contains an unescaped <script>")
	}
	if empty := string(renderMatrixHTML(nil)); !strings.Contains(empty, "no deploy records") {
		t.Error("empty matrix html missing the no-deploy-records row")
	}
}

func TestRenderMatrixDeterministic(t *testing.T) {
	if !bytes.Equal(renderMatrixMD(matrixFixture), renderMatrixMD(matrixFixture)) {
		t.Error("md renderer is not deterministic")
	}
	if !bytes.Equal(renderMatrixHTML(matrixFixture), renderMatrixHTML(matrixFixture)) {
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
