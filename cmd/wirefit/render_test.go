package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
)

var matrixFixture = []matrixEdge{
	{Env: "prod", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order", Status: matrixStatusOK},
	{Env: "dev", Consumer: "web-app", Provider: "order-service", Interaction: "orders.get-order", Status: matrixStatusOK},
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

// inventoryFixture mirrors the deploy records behind matrixFixture, plus
// recommendation-service: a provider nobody consumes, visible only here.
var inventoryFixture = []invService{
	{Service: "billing", Envs: []invEnv{
		{Env: "dev", RecordedAt: "2026-04-20T10:00:00Z", RecordedBy: "erin",
			Provides: []invItem{{Key: "invoices.get", Record: &deployRecord{
				RecordedAt: "2026-04-20T10:00:00Z", RecordedBy: "erin", Hash: "aaaaaaaaaaaa", Version: 5}}}},
	}},
	{Service: "mobile", Envs: []invEnv{
		{Env: "staging", RecordedAt: "2026-05-01T12:00:00Z", RecordedBy: "alice",
			Consumes: []invItem{
				{Key: "billing/invoices.get", Record: &deployRecord{
					RecordedAt: "2026-05-02T08:00:00Z", RecordedBy: "carol", Hash: "feedbeefcafe"}},
				{Key: "order-service/orders.get-order", Record: &deployRecord{
					RecordedAt: "2026-05-01T12:00:00Z", RecordedBy: "alice", Hash: "abc123def456", Version: 3}},
			}},
	}},
	{Service: "order-service", Envs: []invEnv{
		{Env: "dev", RecordedAt: "2026-05-02T09:00:00Z", RecordedBy: "erin",
			Provides: []invItem{{Key: "orders.get-order", Record: &deployRecord{
				RecordedAt: "2026-05-02T09:00:00Z", RecordedBy: "erin", Hash: "222222222222", Version: 8}}}},
		{Env: "prod", RecordedAt: "2026-03-01T09:00:00Z", RecordedBy: "bob", Stale: true,
			Provides: []invItem{{Key: "orders.get-order", Record: &deployRecord{
				RecordedAt: "2026-03-01T09:00:00Z", RecordedBy: "bob", Hash: "444444444444", Version: 6}}}},
		{Env: "staging", RecordedAt: "2026-04-28T09:30:00Z", RecordedBy: "bob",
			Provides: []invItem{{Key: "orders.get-order", Record: &deployRecord{
				RecordedAt: "2026-04-28T09:30:00Z", RecordedBy: "bob", Hash: "0123456789ab", Version: 7}}}},
	}},
	{Service: "recommendation-service", Envs: []invEnv{
		{Env: "dev", RecordedAt: "2026-05-05T08:00:00Z", RecordedBy: "gale",
			Provides: []invItem{{Key: "recs.list", Record: &deployRecord{
				RecordedAt: "2026-05-05T08:00:00Z", RecordedBy: "gale", Hash: "555555555555", Version: 1}}}},
		// Version-less record: published before version logs existed.
		{Env: "prod", RecordedAt: "2026-05-06T08:00:00Z", RecordedBy: "gale",
			Provides: []invItem{{Key: "recs.list", Record: &deployRecord{
				RecordedAt: "2026-05-06T08:00:00Z", RecordedBy: "gale", Hash: "666666666666"}}}},
	}},
	{Service: "web-app", Envs: []invEnv{
		{Env: "dev", RecordedAt: "2026-05-03T10:00:00Z", RecordedBy: "dana",
			Consumes: []invItem{
				{Key: "billing/invoices.get", Record: &deployRecord{
					RecordedAt: "2026-05-03T10:00:00Z", RecordedBy: "dana", Hash: "777777777777", Version: 2}},
				{Key: "order-service/orders.get-order", Record: &deployRecord{
					RecordedAt: "2026-05-03T10:00:00Z", RecordedBy: "dana", Hash: "111111111111", Version: 4}},
			}},
		{Env: "prod", RecordedAt: "2026-05-04T10:00:00Z", RecordedBy: "dana",
			Consumes: []invItem{{Key: "order-service/orders.get-order", Record: &deployRecord{
				RecordedAt: "2026-05-04T10:00:00Z", RecordedBy: "dana", Hash: "888888888888", Version: 3}}}},
		{Env: "staging", RecordedAt: "2026-05-05T10:00:00Z", RecordedBy: "dana",
			Consumes: []invItem{{Key: "order-service/orders.get-order", Record: &deployRecord{
				RecordedAt: "2026-05-05T10:00:00Z", RecordedBy: "dana", Hash: "999999999999", Version: 3}}}},
	}},
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
	out := string(renderMatrixHTML(matrixFixture, nil, nil, nil))
	for _, want := range []string{
		// The overview is the no-JS fallback and every major directory is a
		// fragment-addressed view.
		`.view:target { display: block; }`,
		`.view:target ~ #view-overview { display: none; }`,
		`<section class="view directory" id="view-contracts" data-directory>`,
		`<section class="view" id="view-services">`,
		`<section class="view" id="view-overview">`,
		`<h1>Compatibility overview</h1><p class="verdict">2 failing edges in dev, staging</p>`,
		`<strong>2</strong><span>deployed blockers</span>`,
		`<div class="filters" hidden>`,
		`data-search="order-service orders.get-order" data-status="INCOMPATIBLE"`,
		`<a href="#c-order-service-orders.get-order"><code>order-service / orders.get-order</code></a>`,
		// Contract health and its provider ownership are explicit.
		`<section class="view" id="c-order-service-orders.get-order">`,
		`<a href="#s-order-service">order-service</a>`,
		`<span class="detail">Deployed</span><span class="badge st-INCOMPATIBLE"`,
		`<h3>Environment summary</h3>`,
		`data-label="environment">staging</td><td data-label="provider version" class="ver"><code title="0123456789ab`,
		`<h3>Deployed relationships</h3>`,
		`<a href="#edge-c-order-service-orders.get-order-e2"><code>mobile</code></a>`,
		// Edge results are shareable pages with findings before secondary data.
		`<section class="view" id="edge-c-order-service-orders.get-order-e2">`,
		`<h1>mobile → order-service/orders.get-order</h1>`,
		`<span>Consumer</span><strong><code>mobile</code></strong>`,
		"field removed &lt;script&gt;alert(1)&lt;/script&gt;",
		`<code>field-missing</code>`,
		`<details class="disclosure"><summary>Deploy provenance</summary>`,
		`<details class="disclosure"><summary>Compare schemas</summary>`,
		`<span class="hl st-warning">    &#34;qty&#34;: {</span>`,
		`<span class="hl st-INCOMPATIBLE">      &#34;items&#34;: {</span>`,
		`<span class="hl st-INCOMPATIBLE">          &#34;sku&#34;: {</span>`,
		`recorded <time datetime="2026-05-01T12:00:00Z">01 May 2026, 12:00 UTC</time> by alice`,
		`<nav class="pipeline" aria-label="Environment pipeline">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<script>alert(1)") {
		t.Error("html output contains an unescaped <script> payload")
	}
	for _, gone := range []string{"data-modal", "<dialog", `data-href=`} {
		if strings.Contains(out, gone) {
			t.Errorf("html output contains obsolete interaction marker %q", gone)
		}
	}
	if strings.Contains(out, `<span class="parrow`) {
		t.Error("html output must not render promotion arrows without a pipeline")
	}
	if strings.Contains(out, "<th>promotion</th>") {
		t.Error("html output must omit the promotion column without a pipeline")
	}
	if strings.Contains(out, "<h3>promotion") {
		t.Error("html output must omit the promotion sections without promotion checks")
	}
	if empty := string(renderMatrixHTML(nil, nil, nil, nil)); !strings.Contains(empty, "no deploy records") {
		t.Error("empty matrix html missing the no-deploy-records card")
	} else if strings.Contains(empty, `<nav class="pipeline">`) {
		t.Error("empty matrix html must not render a pipeline strip")
	}
}

func TestRenderMatrixHTMLPromotions(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, promoFixture, []string{"dev", "staging", "prod"}, nil))
	for _, want := range []string{
		`<strong>2</strong><span>promotion issues</span>`,
		// Deployed and promotion status are separate on contract summaries.
		`data-label="deployed health"><span class="badge st-INCOMPATIBLE"`,
		`data-label="promotion readiness"><span class="badge st-INCOMPATIBLE"`,
		`<span class="detail">Deployed</span><span class="badge st-INCOMPATIBLE"`,
		`<span class="detail">Promotion</span><span class="badge st-INCOMPATIBLE"`,
		`data-label="promotion"><span class="detail">to staging</span>`,
		`<div class="detail">1 untracked</div>`,
		`<h3>Promotion checks</h3>`,
		`<a href="#promotion-c-order-service-orders.get-order-p0"><code>provides orders.get-order ⇐ web-app</code></a>`,
		"parser requires field &lt;b&gt;a&lt;/b&gt;",
		`<section class="view" id="promotion-c-order-service-orders.get-order-p0">`,
		`<a class="back-link" href="#c-order-service-orders.get-order" aria-label="Back to contract" title="Back to contract"><span aria-hidden="true">←</span></a>`,
		`<p class="eyebrow">Promotion compatibility</p><h1>web-app → order-service/orders.get-order</h1>`,
		`<h4>target consumer · staging</h4>`,
		`<h4>candidate provider · dev</h4>`,
		`provider version <code>v8</code> · hash <code>222222222222</code>`,
		`<section class="view" id="promotion-c-billing-invoices.get-p0">`,
		`<h1>order-service → billing/invoices.get</h1>`,
		`<h4>candidate consumer · staging</h4>`,
		`<h4>target provider · prod</h4>`,
		`consumer version <code>v2</code> · hash <code>333333333333</code>`,
		`<code>untracked-provider</code>`,
		`<span class="parrow pa-INCOMPATIBLE" title="promote dev → staging: 1 INCOMPATIBLE · 1 ok">→</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	// The directory is worst-first by the combined attention status while the
	// two displayed dimensions remain separate.
	worst := strings.Index(out, `data-search="order-service orders.get-order"`)
	next := strings.Index(out, `data-search="billing invoices.get"`)
	if !(worst >= 0 && next >= 0 && worst < next) {
		t.Errorf("landing order wrong: order-service=%d billing=%d", worst, next)
	}
}

func TestRenderMatrixHTMLPipeline(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, promoFixture, []string{"dev", "staging", "qa"}, nil))
	for _, want := range []string{
		`<strong>dev</strong><span class="psum">1 error · 1 ok</span>`,
		`<strong>staging</strong><span class="psum">1 INCOMPATIBLE · 1 warning · 1 untracked</span>`,
		`<span class="parrow pa-INCOMPATIBLE" title="promote dev → staging: 1 INCOMPATIBLE · 1 ok">→</span>`,
		`<span class="parrow pa-untracked" title="promote staging → qa: no promotion checks">→</span>`,
		`<strong>qa</strong><span class="psum">no deploy records</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	overview := out[strings.Index(out, `id="view-overview"`):]
	dev := strings.Index(overview, `<strong>dev</strong><span class="psum">`)
	staging := strings.Index(overview, `<strong>staging</strong><span class="psum">`)
	qa := strings.Index(overview, `<strong>qa</strong><span class="psum">`)
	prod := strings.Index(overview, `<strong>prod</strong><span class="psum">`)
	if !(dev < staging && staging < qa && qa < prod) {
		t.Errorf("strip order wrong: dev=%d staging=%d qa=%d prod=%d", dev, staging, qa, prod)
	}
	dev = strings.Index(out, `data-label="environment">dev</td>`)
	staging = strings.Index(out, `data-label="environment">staging</td>`)
	prod = strings.Index(out, `data-label="environment">prod</td>`)
	if !(dev >= 0 && dev < staging && staging < prod) {
		t.Errorf("environments table order wrong: dev=%d staging=%d prod=%d", dev, staging, prod)
	}
}

func TestPromoRef(t *testing.T) {
	tests := []struct {
		edge promoEdge
		want contractRef
		ok   bool
	}{
		{promoEdge{Side: "provides", Service: "order-service", Counterpart: "web-app", Interaction: "orders.get"},
			contractRef{"order-service", "orders.get"}, true},
		{promoEdge{Side: "consumes", Service: "web-app", Counterpart: "order-service", Interaction: "orders.get"},
			contractRef{"order-service", "orders.get"}, true},
		// Per-service rollup rows carry no interaction and match no contract.
		{promoEdge{Service: "web-app", InSync: true}, contractRef{}, false},
	}
	for _, tt := range tests {
		if got, ok := promoRef(tt.edge); got != tt.want || ok != tt.ok {
			t.Errorf("promoRef(%+v) = %v, %v; want %v, %v", tt.edge, got, ok, tt.want, tt.ok)
		}
	}
}

func TestContractSlugs(t *testing.T) {
	refs := []contractRef{{"a-b", "c"}, {"a", "b-c"}, {"svc", "weird interaction!"}}
	slugs := contractSlugs(refs)
	if slugs[refs[0]] != "c-a-b-c" || slugs[refs[1]] != "c-a-b-c-2" {
		t.Errorf("colliding refs not deduped deterministically: %v", slugs)
	}
	idSafe := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)
	for ref, s := range slugs {
		if !idSafe.MatchString(s) {
			t.Errorf("slug %q for %v is not DOM-id safe", s, ref)
		}
	}
}

func TestPromoCell(t *testing.T) {
	rollup := promoEdge{From: "dev", To: "staging", Service: "billing", Status: matrixStatusOK, InSync: true,
		Detail: "in sync: the same contracts are already deployed in staging"}
	matched := []promoEdge{
		{From: "dev", To: "staging", Status: matrixStatusWarning},
		{From: "dev", To: "staging", Status: matrixStatusOK},
		{From: "staging", To: "prod", Status: matrixStatusIncompatible},
	}
	if st, detail := promoCell(matched, nil, "billing", "dev", "staging"); st != matrixStatusWarning || detail != "1 warning · 1 ok" {
		t.Errorf("matched checks: got %s %q", st, detail)
	}
	// The in-sync collapse leaves only the provider's per-service row to
	// speak for the contract.
	if st, detail := promoCell(nil, []promoEdge{rollup}, "billing", "dev", "staging"); st != matrixStatusOK || !strings.Contains(detail, "in sync") {
		t.Errorf("rollup fallback: got %s %q", st, detail)
	}
	// A rollup for a different service says nothing about this contract.
	if st, detail := promoCell(nil, []promoEdge{rollup}, "order-service", "dev", "staging"); st != matrixStatusUntracked || detail != "no promotion checks" {
		t.Errorf("no checks: got %s %q", st, detail)
	}
}

func TestRenderMatrixHTMLServices(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, nil, nil, inventoryFixture))
	for _, want := range []string{
		`<p class="sub">3 contracts · 5 services · 3 envs</p>`,
		`<nav class="topnav" aria-label="Primary"><a href="#view-overview">Overview</a><a href="#view-contracts">Contracts</a><a href="#view-services" aria-current="page">Services</a></nav>`,
		`<section class="view" id="view-services">`,
		`data-search="mobile consumer" data-status="INCOMPATIBLE"`,
		`data-search="order-service provider" data-status="INCOMPATIBLE"`,
		`data-search="web-app consumer" data-status="error"`,
		`<section class="view" id="s-recommendation-service">`,
		`<nav class="breadcrumbs" aria-label="Breadcrumb"><a class="back-link" href="#view-services" aria-label="Back to services" title="Back to services"><span aria-hidden="true">←</span></a><a href="#view-services">Services</a><span>/</span><span>recommendation-service</span></nav>`,
		`<h1><code>recommendation-service</code></h1>`,
		`<p class="sub">provider · 1 provided interaction · 0 consumed contracts</p>`,
		`<time datetime="2026-05-05T08:00:00Z">05 May 2026, 08:00 UTC</time>`,
		`<a href="#c-recommendation-service-recs.list"><code>recs.list</code></a>`,
		`<span class="badge st-warning">stale</span>`,
		// A consumer badge links to its exact edge and carries only that
		// consumer's status, not the contract-wide worst status.
		`<a class="badge st-warning" href="#edge-c-order-service-orders.get-order-e3" title="999999999999 · recorded 2026-05-05T10:00:00Z by dana">staging v3</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	// The sibling CSS depends on the overview being the last view.
	services, overview := strings.Index(out, `id="view-services"`), strings.Index(out, `id="view-overview"`)
	if !(services >= 0 && overview >= 0 && services < overview) {
		t.Errorf("view order wrong: services=%d overview=%d", services, overview)
	}
	// Services rows are worst-first then name.
	mobile := strings.Index(out, `data-search="mobile consumer"`)
	orderSvc := strings.Index(out, `data-search="order-service provider"`)
	webApp := strings.Index(out, `data-search="web-app consumer"`)
	recs := strings.Index(out, `data-search="recommendation-service provider"`)
	if !(mobile < orderSvc && orderSvc < webApp && webApp < recs) {
		t.Errorf("services order wrong: mobile=%d order-service=%d web-app=%d recs=%d", mobile, orderSvc, webApp, recs)
	}
	if n := strings.Count(out, `<section class="view" id="edge-`); n != len(matrixFixture) {
		t.Errorf("edge detail views = %d, want %d", n, len(matrixFixture))
	}
	start := strings.Index(out, `<section class="view" id="s-order-service">`)
	end := start + strings.Index(out[start:], `</section>`)
	service := out[start:end]
	for _, want := range []string{
		`<h3>Deployed issues</h3>`,
		`data-label="where">staging</td><td data-label="role">provider</td>`,
		`<a href="#edge-c-order-service-orders.get-order-e2"><code>mobile → order-service/orders.get-order</code></a>`,
		`field removed &lt;script&gt;alert(1)&lt;/script&gt;`,
	} {
		if !strings.Contains(service, want) {
			t.Errorf("order-service view missing %q\n%s", want, service)
		}
	}
}

func TestRenderMatrixHTMLServiceFiltersUseServiceStatuses(t *testing.T) {
	record := &deployRecord{Hash: "aaaaaaaaaaaa"}
	edges := []matrixEdge{
		{Env: "dev", Consumer: "bad-client", Provider: "provider", Interaction: "get", Status: matrixStatusWarning},
		{Env: "dev", Consumer: "good-client", Provider: "provider", Interaction: "get", Status: matrixStatusOK},
	}
	inv := []invService{
		{Service: "bad-client", Envs: []invEnv{{Env: "dev", Consumes: []invItem{{Key: "provider/get", Record: record}}}}},
		{Service: "good-client", Envs: []invEnv{{Env: "dev", Consumes: []invItem{{Key: "provider/get", Record: record}}}}},
		{Service: "provider", Envs: []invEnv{{Env: "dev", Provides: []invItem{{Key: "get", Record: record}}}}},
	}
	out := string(renderMatrixHTML(edges, nil, nil, inv))
	if n := strings.Count(out, `<button type="button" data-status="ok" aria-pressed="true">ok</button>`); n != 1 {
		t.Errorf("ok filter count = %d, want one service-directory filter", n)
	}
}

func TestBuildServicesUsesConsumerSpecificStatus(t *testing.T) {
	slugs := map[contractRef]string{
		{"order-service", "orders.get-order"}:   "c-order",
		{"billing", "invoices.get"}:             "c-billing",
		{"recommendation-service", "recs.list"}: "c-recs",
	}
	details := map[edgeRef]string{
		{"staging", "web-app", "order-service", "orders.get-order"}: "edge-web-staging",
	}
	_, views := buildServices(inventoryFixture, matrixFixture, nil, slugs, details, nil, envLess(nil))

	var web, order *serviceView
	for i := range views {
		switch views[i].Service {
		case "web-app":
			web = &views[i]
		case "order-service":
			order = &views[i]
		}
	}
	if web == nil || order == nil {
		t.Fatalf("missing service views: web=%v order=%v", web != nil, order != nil)
	}
	findEnv := func(rows []svcItemRow, label, env string) svcItemEnv {
		for _, row := range rows {
			if row.Label != label {
				continue
			}
			for _, item := range row.Envs {
				if item.Env == env {
					return item
				}
			}
		}
		t.Fatalf("missing %s in %s", label, env)
		return svcItemEnv{}
	}
	consumed := findEnv(web.ConsumeRows, "order-service/orders.get-order", "staging")
	if consumed.Status != matrixStatusWarning || consumed.DetailSlug != "edge-web-staging" {
		t.Errorf("web-app staging consume = %+v, want warning with exact edge link", consumed)
	}
	provided := findEnv(order.ProvideRows, "orders.get-order", "staging")
	if provided.Status != matrixStatusIncompatible {
		t.Errorf("provider aggregate status = %s, want INCOMPATIBLE", provided.Status)
	}
}

func TestBuildServiceAttentionIncludesPromotionReason(t *testing.T) {
	details := map[promoDetailRef]string{
		{"dev", "staging", "order-service", "provides", "web-app", "orders.get-order"}: "promotion-order",
	}
	_, groups, hasChecks := buildServiceAttention("order-service", nil, promoFixture, nil, details,
		envLess([]string{"dev", "staging", "prod"}))
	if !hasChecks || len(groups) != 2 {
		t.Fatalf("promotion groups = %+v, has checks = %v, want two transition groups", groups, hasChecks)
	}
	item := groups[0].Rows[0]
	if groups[0].Label != "dev → staging" || groups[0].Status != matrixStatusIncompatible ||
		item.Role != "provider" ||
		item.Relationship != "web-app → order-service/orders.get-order" ||
		item.Status != matrixStatusIncompatible || item.DetailSlug != "promotion-order" ||
		!strings.Contains(item.Detail, "parser requires field") {
		t.Errorf("provider promotion attention = %+v", item)
	}
}

func TestRenderMatrixHTMLServicePromotionGroups(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, promoFixture,
		[]string{"dev", "staging", "prod"}, inventoryFixture))
	start := strings.Index(out, `<section class="view" id="s-order-service">`)
	end := start + 1 + strings.Index(out[start+1:], `
<section class="view"`)
	service := out[start:end]
	for _, want := range []string{
		`<h3>Promotion readiness</h3>`,
		`<strong>dev → staging</strong><span>1 issue requiring attention</span>`,
		`aria-label="Blocked: 1 issue requiring attention"`,
		`<strong>staging → prod</strong><span>1 of 1 check could not be verified</span>`,
		`aria-label="Unverified: 1 of 1 check could not be verified"`,
		`<a href="#promotion-c-order-service-orders.get-order-p0"><code>web-app → order-service/orders.get-order</code></a>`,
		`parser requires field &lt;b&gt;a&lt;/b&gt;`,
	} {
		if !strings.Contains(service, want) {
			t.Errorf("order-service promotion view missing %q\n%s", want, service)
		}
	}
	start = strings.Index(out, `<section class="view" id="s-web-app">`)
	end = start + 1 + strings.Index(out[start+1:], `
<section class="view"`)
	service = out[start:end]
	for _, want := range []string{
		`<section class="attention-group is-clear">`,
		`<strong>dev → staging</strong><span>1 compatibility check passed</span>`,
		`aria-label="Ready: 1 compatibility check passed"`,
	} {
		if !strings.Contains(service, want) {
			t.Errorf("web-app promotion view missing %q\n%s", want, service)
		}
	}
}

func TestPromotionGroupOutcome(t *testing.T) {
	tests := []struct {
		name     string
		statuses []matrixStatus
		issues   int
		status   matrixStatus
		outcome  string
		summary  string
	}{
		{"ready", []matrixStatus{matrixStatusOK, matrixStatusOK}, 0,
			matrixStatusOK, "Ready", "All 2 compatibility checks passed"},
		{"warnings", []matrixStatus{matrixStatusOK, matrixStatusWarning}, 1,
			matrixStatusWarning, "Ready with warnings", "Compatible with 1 warning"},
		{"unverified", []matrixStatus{matrixStatusWarning, matrixStatusUntracked}, 2,
			matrixStatusUntracked, "Unverified", "1 of 2 checks could not be verified"},
		{"blocked", []matrixStatus{matrixStatusIncompatible, matrixStatusUntracked}, 2,
			matrixStatusIncompatible, "Blocked", "2 issues requiring attention"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, outcome, summary := promotionGroupOutcome(tt.statuses, tt.issues)
			if status != tt.status || outcome != tt.outcome || summary != tt.summary {
				t.Errorf("promotion outcome = %s, %q, %q; want %s, %q, %q",
					status, outcome, summary, tt.status, tt.outcome, tt.summary)
			}
		})
	}
}

func TestRenderMatrixHTMLInventoryContract(t *testing.T) {
	out := string(renderMatrixHTML(matrixFixture, nil, nil, inventoryFixture))
	for _, want := range []string{
		// A provider nobody consumes still lands in the contract directory.
		`data-search="recommendation-service recs.list" data-status="untracked" data-envs="dev prod "`,
		`<a href="#c-recommendation-service-recs.list"><code>recommendation-service / recs.list</code></a>`,
		`<section class="view" id="c-recommendation-service-recs.list">`,
		`<a class="back-link" href="#view-contracts" aria-label="Back to contracts" title="Back to contracts"><span aria-hidden="true">←</span></a>`,
		`data-label="environment">dev</td><td data-label="provider version" class="ver"><code title="555555555555 · recorded 2026-05-05T08:00:00Z by gale">v1</code>`,
		`<code title="666666666666 · recorded 2026-05-06T08:00:00Z by gale">666666666666</code>`,
		`data-label="provider version" class="ver"><code title="aaaaaaaaaaaa · recorded 2026-04-20T10:00:00Z by erin">v5</code>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n%s", want, out)
		}
	}
	// No consumer tables on a contract without consumers.
	start := strings.Index(out, `id="c-recommendation-service-recs.list"`)
	end := start + strings.Index(out[start:], "</section>")
	if view := out[start:end]; strings.Contains(view, "<h3>Deployed relationships</h3>") {
		t.Errorf("consumer-less contract view has a consumers section:\n%s", view)
	}
}

func TestServiceSlugs(t *testing.T) {
	slugs := serviceSlugs([]string{"a-b", "a/b", "svc"})
	if slugs["a-b"] != "s-a-b" || slugs["a/b"] != "s-a-b-2" || slugs["svc"] != "s-svc" {
		t.Errorf("colliding names not deduped deterministically: %v", slugs)
	}
	idSafe := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)
	for name, s := range slugs {
		if !idSafe.MatchString(s) {
			t.Errorf("slug %q for %q is not DOM-id safe", s, name)
		}
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
	if !bytes.Equal(renderMatrixHTML(matrixFixture, promoFixture, pipeline, inventoryFixture), renderMatrixHTML(matrixFixture, promoFixture, pipeline, inventoryFixture)) {
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
