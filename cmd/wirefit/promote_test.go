package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/store"
)

// Fixture IR: emitters/parsers over fields a (string) and b (int32).
const (
	irAB = `{"type":"object","properties":{"a":{"type":"string","x-ct-scalar":"string"},"b":{"type":"integer","x-ct-scalar":"int32"}},"required":["a","b"]}`
	irA  = `{"type":"object","properties":{"a":{"type":"string","x-ct-scalar":"string"}},"required":["a"]}`
	irB  = `{"type":"object","properties":{"b":{"type":"integer","x-ct-scalar":"int32"}},"required":["b"]}`
)

func writeRepoFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// promoRepo builds the shared contracts-repo fixture: order-service provides
// orders.get; web-app consumes it plus billing (published, unrecorded),
// phantom (declared, unpublished) and ghost (no manifest); mobile is a
// strict consumer registered at main only.
func promoRepo(t *testing.T) *store.Store {
	t.Helper()
	repo := t.TempDir()
	writeRepoFile(t, repo, "contracts/order-service/manifest.yaml", `service: order-service
schema-version: 1
provides:
  - id: orders.get
    kind: rest
    direction: response
    dto: X
`)
	writeRepoFile(t, repo, "contracts/order-service/provides/orders.get.ir.json", irAB)
	writeRepoFile(t, repo, "contracts/billing/manifest.yaml", `service: billing
schema-version: 1
provides:
  - id: invoices.get
    kind: rest
    direction: response
    dto: X
`)
	writeRepoFile(t, repo, "contracts/billing/provides/invoices.get.ir.json", irAB)
	writeRepoFile(t, repo, "contracts/phantom/manifest.yaml", `service: phantom
schema-version: 1
provides:
  - id: things.list
    kind: rest
    direction: response
    dto: X
`)
	writeRepoFile(t, repo, "contracts/web-app/manifest.yaml", `service: web-app
schema-version: 1
consumes:
  - id: orders.get
    provider: order-service
    dto: Y
  - id: invoices.get
    provider: billing
    dto: Y
  - id: things.list
    provider: phantom
    dto: Y
  - id: stuff.get
    provider: ghost
    dto: Y
`)
	writeRepoFile(t, repo, "contracts/web-app/consumes/order-service/orders.get.ir.json", irA)
	writeRepoFile(t, repo, "contracts/web-app/consumes/billing/invoices.get.ir.json", irA)
	writeRepoFile(t, repo, "contracts/web-app/consumes/phantom/things.list.ir.json", irA)
	writeRepoFile(t, repo, "contracts/web-app/consumes/ghost/stuff.get.ir.json", irA)
	writeRepoFile(t, repo, "contracts/mobile/manifest.yaml", `service: mobile
schema-version: 1
settings:
  unknown-fields: reject
consumes:
  - id: orders.get
    provider: order-service
    dto: Y
`)
	writeRepoFile(t, repo, "contracts/mobile/consumes/order-service/orders.get.ir.json", irA)

	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func blob(t *testing.T, st *store.Store, raw string) string {
	t.Helper()
	h, err := st.WriteBlob([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func schema(t *testing.T, raw string) *ir.Schema {
	t.Helper()
	s, err := ir.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func saveLock(t *testing.T, st *store.Store, env string, lock store.EnvLock) {
	t.Helper()
	if err := st.SaveEnvLock(env, lock); err != nil {
		t.Fatal(err)
	}
}

func neverStale() time.Time { return time.Now().Add(-24 * time.Hour) }

func TestEvalDeployKindsAndLabels(t *testing.T) {
	st := promoRepo(t)
	lock := store.EnvLock{
		"web-app": {RecordedAt: time.Now(), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}},
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{
			"orders.get": blob(t, st, irAB),
		}},
	}

	// Provider side: candidate drops field a, breaking both consumers.
	prov := candidate{service: "order-service", provides: []candProvide{
		{id: "orders.get", dir: diff.P2C, schema: schema(t, irB)},
	}}
	drs, err := evalDeploy(st, prov, "staging", lock, neverStale(), 30)
	if err != nil {
		t.Fatal(err)
	}
	wantProv := []struct {
		label string
		kind  trackedKind
		max   diff.Class
		rule  string // a rule that must appear in the findings
	}{
		{"provides orders.get ⇐ web-app@staging", tracked, diff.Breaking, "field-missing"},
		{"provides orders.get ⇐ mobile (untracked)", untrackedMain, diff.Breaking, "untracked-consumer"},
	}
	if len(drs) != len(wantProv) {
		t.Fatalf("got %d results, want %d: %+v", len(drs), len(wantProv), drs)
	}
	for i, want := range wantProv {
		dr := drs[i]
		if got := canIDeployLabel(dr, "staging"); got != want.label {
			t.Errorf("label[%d] = %q, want %q", i, got, want.label)
		}
		if dr.kind != want.kind || dr.res.Max() != want.max {
			t.Errorf("%s: kind, max = %v, %v, want %v, %v", want.label, dr.kind, dr.res.Max(), want.kind, want.max)
		}
		found := false
		for _, f := range dr.res.Findings {
			found = found || f.Rule == want.rule
		}
		if !found {
			t.Errorf("%s: missing rule %s in %+v", want.label, want.rule, dr.res.Findings)
		}
	}

	// Consumer side: one candidate consumption per trackedKind.
	cons := candidate{service: "web-app", consumes: []candConsume{
		{provider: "order-service", id: "orders.get", schema: schema(t, irA)},
		{provider: "billing", id: "invoices.get", schema: schema(t, irA)},
		{provider: "phantom", id: "things.list", schema: schema(t, irA)},
		{provider: "ghost", id: "stuff.get", schema: schema(t, irA)},
	}}
	drs, err = evalDeploy(st, cons, "staging", lock, neverStale(), 30)
	if err != nil {
		t.Fatal(err)
	}
	wantCons := []struct {
		label string
		kind  trackedKind
		max   diff.Class
	}{
		{"consumes order-service/orders.get @staging", tracked, diff.Neutral},
		{"consumes billing/invoices.get (untracked)", untrackedMain, diff.Warning},
		{"consumes phantom/things.list @staging", untrackedNone, diff.Warning},
		{"consumes ghost/stuff.get", unpublished, diff.Warning},
	}
	if len(drs) != len(wantCons) {
		t.Fatalf("got %d results, want %d: %+v", len(drs), len(wantCons), drs)
	}
	for i, want := range wantCons {
		dr := drs[i]
		if got := canIDeployLabel(dr, "staging"); got != want.label {
			t.Errorf("label[%d] = %q, want %q", i, got, want.label)
		}
		if dr.kind != want.kind || dr.res.Max() != want.max {
			t.Errorf("%s: kind, max = %v, %v, want %v, %v", want.label, dr.kind, dr.res.Max(), want.kind, want.max)
		}
	}
}

func TestEvalDeployStaleRecord(t *testing.T) {
	st := promoRepo(t)
	lock := store.EnvLock{
		"web-app": {RecordedAt: time.Now().Add(-90 * 24 * time.Hour), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}},
	}
	prov := candidate{service: "order-service", provides: []candProvide{
		{id: "orders.get", dir: diff.P2C, schema: schema(t, irAB)},
	}}
	drs, err := evalDeploy(st, prov, "staging", lock, time.Now().Add(-30*24*time.Hour), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, dr := range drs {
		if dr.counterpart != "web-app" {
			continue
		}
		for _, f := range dr.res.Findings {
			if f.Rule == "stale-deploy-record" {
				return
			}
		}
		t.Fatalf("web-app result missing stale-deploy-record: %+v", dr.res.Findings)
	}
	t.Fatal("no result for web-app")
}

func TestEvalDeployDeterministic(t *testing.T) {
	st := promoRepo(t)
	lock := store.EnvLock{}
	for _, svc := range []string{"web-app", "mobile", "kiosk"} {
		lock[svc] = &store.ServiceLock{RecordedAt: time.Now(), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}}
	}
	prov := candidate{service: "order-service", provides: []candProvide{
		{id: "orders.get", dir: diff.P2C, schema: schema(t, irAB)},
	}}
	order := func() []string {
		drs, err := evalDeploy(st, prov, "staging", lock, neverStale(), 30)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, dr := range drs {
			out = append(out, dr.counterpart)
		}
		return out
	}
	first := order()
	want := []string{"kiosk", "mobile", "web-app"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("order = %v, want %v", first, want)
	}
	for range 5 {
		if got := order(); !reflect.DeepEqual(got, first) {
			t.Fatalf("order changed between runs: %v vs %v", got, first)
		}
	}
}

func TestCandidateFromLock(t *testing.T) {
	st := promoRepo(t)
	// direction request → C2P must come from the published manifest.
	writeRepoFile(t, st.Dir, "contracts/uploader/manifest.yaml", `service: uploader
schema-version: 1
settings:
  unknown-fields: reject
provides:
  - id: files.put
    kind: rest
    direction: request
    dto: X
`)
	sl := &store.ServiceLock{
		Provides: map[string]string{"files.put": blob(t, st, irAB)},
		Consumes: map[string]string{"billing/invoices.get": blob(t, st, irA)},
	}
	c, err := candidateFromLock(st, "uploader", sl)
	if err != nil {
		t.Fatal(err)
	}
	if !c.rejectsUnknown {
		t.Error("rejectsUnknown must come from the published manifest")
	}
	if len(c.provides) != 1 || c.provides[0].dir != diff.C2P {
		t.Errorf("provides = %+v, want one C2P entry", c.provides)
	}
	if len(c.consumes) != 1 || c.consumes[0].provider != "billing" || c.consumes[0].id != "invoices.get" {
		t.Errorf("consumes = %+v", c.consumes)
	}

	// No published manifest: direction defaults to P2C, like matrixEdges.
	c, err = candidateFromLock(st, "stranger", &store.ServiceLock{
		Provides: map[string]string{"x.y": blob(t, st, irAB)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.provides[0].dir != diff.P2C || c.rejectsUnknown {
		t.Errorf("defaults: dir = %v, rejectsUnknown = %v, want p2c, false", c.provides[0].dir, c.rejectsUnknown)
	}

	// A pinned blob that does not exist surfaces as an error.
	if _, err := candidateFromLock(st, "uploader", &store.ServiceLock{
		Provides: map[string]string{"files.put": "sha256:deadbeef"},
	}); err == nil {
		t.Error("missing blob must be an error")
	}
}

func TestPromoEdges(t *testing.T) {
	st := promoRepo(t)
	webConsumes := map[string]string{"order-service/orders.get": blob(t, st, irA)}
	saveLock(t, st, "dev", store.EnvLock{
		// order-service@dev dropped field a: promoting it breaks web-app@staging.
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{"orders.get": blob(t, st, irB)}},
		// web-app is pinned to the same hashes in both envs: in sync.
		"web-app": {RecordedAt: time.Now(), Consumes: webConsumes},
		// a pinned blob nobody wrote: the row must degrade, not abort.
		"broken": {RecordedAt: time.Now(), Provides: map[string]string{"x.y": "sha256:deadbeef"}},
	})
	saveLock(t, st, "staging", store.EnvLock{
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{"orders.get": blob(t, st, irAB)}},
		"web-app":       {RecordedAt: time.Now(), Consumes: webConsumes},
	})
	// staging → prod: prod is empty, everything degrades to untracked.

	edges, err := promoEdges(st, []string{"dev", "staging", "prod"}, neverStale(), 30)
	if err != nil {
		t.Fatal(err)
	}
	find := func(from, service, counterpart string) *promoEdge {
		for i := range edges {
			e := &edges[i]
			if e.From == from && e.Service == service && e.Counterpart == counterpart {
				return e
			}
		}
		t.Fatalf("no edge from=%s service=%s counterpart=%s in %+v", from, service, counterpart, edges)
		return nil
	}

	if e := find("dev", "order-service", "web-app"); e.Status != matrixStatusIncompatible {
		t.Errorf("breaking promotion: status = %s, want INCOMPATIBLE (%+v)", e.Status, e)
	} else if e.ConsumerBody == nil || e.ProviderBody == nil || e.ConsumerRecord == nil || e.ProviderRecord == nil {
		t.Errorf("tracked provider check missing modal bodies or provenance: %+v", e)
	}
	if e := find("dev", "web-app", ""); e.Status != matrixStatusOK || !e.InSync || e.Detail == "" {
		t.Errorf("in-sync service: %+v, want an ok row with detail", e)
	}
	if e := find("dev", "broken", ""); e.Status != matrixStatusError {
		t.Errorf("missing blob: status = %s, want error (%+v)", e.Status, e)
	}
	if e := find("staging", "order-service", "web-app"); e.Status != matrixStatusUntracked {
		t.Errorf("promotion into empty env: status = %s, want untracked (%+v)", e.Status, e)
	}
	// web-app@staging is NOT in sync with empty prod: it gets real checks.
	if e := find("staging", "web-app", "order-service"); e.Status != matrixStatusUntracked {
		t.Errorf("consumer into empty env: status = %s, want untracked (%+v)", e.Status, e)
	} else if e.ConsumerBody == nil || e.ProviderBody == nil || e.ConsumerRecord == nil || e.ProviderRecord != nil {
		t.Errorf("untracked consumer check has wrong modal bodies or provenance: %+v", e)
	}

	// Pairs stay in pipeline order; rows sort by service within a pair.
	var fromSeq []string
	for _, e := range edges {
		if len(fromSeq) == 0 || fromSeq[len(fromSeq)-1] != e.From {
			fromSeq = append(fromSeq, e.From)
		}
	}
	if !reflect.DeepEqual(fromSeq, []string{"dev", "staging"}) {
		t.Errorf("pair order = %v, want [dev staging]", fromSeq)
	}

	// Determinism (NF3): identical inputs, identical slice.
	again, err := promoEdges(st, []string{"dev", "staging", "prod"}, neverStale(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(edges, again) {
		t.Error("promoEdges is not deterministic")
	}
}

func TestPromoEdgesInSyncPreservesCompatibility(t *testing.T) {
	st := promoRepo(t)
	provider := map[string]string{"orders.get": blob(t, st, irB)}
	consumer := map[string]string{"order-service/orders.get": blob(t, st, irA)}
	for _, env := range []string{"dev", "staging"} {
		saveLock(t, st, env, store.EnvLock{
			"order-service": {RecordedAt: time.Now(), Provides: provider},
			"web-app":       {RecordedAt: time.Now(), Consumes: consumer},
		})
	}
	edges, err := promoEdges(st, []string{"dev", "staging"}, neverStale(), 30)
	if err != nil {
		t.Fatal(err)
	}
	find := func(service, counterpart string) *promoEdge {
		for i := range edges {
			if edges[i].Service == service && edges[i].Counterpart == counterpart {
				return &edges[i]
			}
		}
		t.Fatalf("no edge service=%s counterpart=%s in %+v", service, counterpart, edges)
		return nil
	}
	for _, e := range []*promoEdge{find("order-service", "web-app"), find("web-app", "order-service")} {
		if !e.InSync || e.Status != matrixStatusIncompatible {
			t.Errorf("in-sync incompatible edge = %+v, want InSync + INCOMPATIBLE", e)
		}
		if !strings.Contains(e.Detail, "in sync:") || !strings.Contains(e.Detail, "parser requires field") {
			t.Errorf("detail = %q, want sync state and true compatibility", e.Detail)
		}
	}
	for _, e := range edges {
		if e.Service == "web-app" && e.Counterpart == "" && e.Status == matrixStatusOK {
			t.Errorf("incompatible in-sync service was collapsed to ok: %+v", e)
		}
	}
}

func TestMatrixEdgesDetail(t *testing.T) {
	st := promoRepo(t)
	chash := blob(t, st, irA)
	phash := blob(t, st, irB) // provider dropped field a: breaking
	saveLock(t, st, "prod", store.EnvLock{
		"web-app": {RecordedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), RecordedBy: "alice",
			Consumes: map[string]string{"order-service/orders.get": chash, "billing/invoices.get": chash}},
		"order-service": {RecordedAt: time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC), RecordedBy: "bob",
			Provides: map[string]string{"orders.get": phash}},
	})
	edges, err := matrixEdges(st, neverStale())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2: %+v", len(edges), edges)
	}

	// edges sort by provider: billing (untracked) before order-service.
	untracked, tracked := edges[0], edges[1]
	if tracked.Status != matrixStatusIncompatible || len(tracked.Findings) == 0 {
		t.Errorf("tracked edge must carry all findings: %+v", tracked)
	}
	wantCons := &deployRecord{RecordedAt: "2026-05-01T12:00:00Z", RecordedBy: "alice", Hash: shortHash(chash)}
	wantProv := &deployRecord{RecordedAt: "2026-04-28T09:30:00Z", RecordedBy: "bob", Hash: shortHash(phash)}
	if !reflect.DeepEqual(tracked.ConsumerRecord, wantCons) || !reflect.DeepEqual(tracked.ProviderRecord, wantProv) {
		t.Errorf("records = %+v / %+v, want %+v / %+v",
			tracked.ConsumerRecord, tracked.ProviderRecord, wantCons, wantProv)
	}
	if h := shortHash(chash); len(h) != 12 || strings.Contains(h, ":") {
		t.Errorf("shortHash(%q) = %q, want 12 bare hex chars", chash, h)
	}
	if untracked.Status != matrixStatusUntracked || untracked.ProviderRecord != nil || untracked.ConsumerRecord == nil {
		t.Errorf("untracked edge must carry consumer provenance only: %+v", untracked)
	}
}

func TestMatrixEdgesVersions(t *testing.T) {
	st := promoRepo(t)
	chash := blob(t, st, irA)
	phash := blob(t, st, irAB)
	saveLock(t, st, "prod", store.EnvLock{
		"web-app": {RecordedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), RecordedBy: "alice",
			Consumes: map[string]string{"order-service/orders.get": chash}},
		"order-service": {RecordedAt: time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC), RecordedBy: "bob",
			Provides: map[string]string{"orders.get": phash}},
	})
	// The consumer's log knows the deployed hash as v1; the provider's log has
	// a newer publish after it, so the deployed hash resolves to v2 of 3.
	if err := st.SaveVersions("web-app", store.VersionLog{
		"consumes/order-service/orders.get": {chash},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveVersions("order-service", store.VersionLog{
		"provides/orders.get": {blob(t, st, irB), phash, blob(t, st, irA)},
	}); err != nil {
		t.Fatal(err)
	}
	edges, err := matrixEdges(st, neverStale())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.ConsumerRecord.Version != 1 || e.ProviderRecord.Version != 2 {
		t.Errorf("versions = v%d / v%d, want v1 / v2", e.ConsumerRecord.Version, e.ProviderRecord.Version)
	}
	if e.ConsumerRecord.Label() != "v1" || e.ProviderRecord.Label() != "v2" {
		t.Errorf("labels = %q / %q, want v1 / v2", e.ConsumerRecord.Label(), e.ProviderRecord.Label())
	}
	// JSON stays additive: Version is omitted when unknown, present when known.
	if data, err := json.Marshal(&deployRecord{Hash: "abc"}); err != nil || strings.Contains(string(data), "Version") {
		t.Errorf("zero Version must be omitted from JSON: %s (%v)", data, err)
	}
	if data, err := json.Marshal(e.ConsumerRecord); err != nil || !strings.Contains(string(data), `"Version":1`) {
		t.Errorf("known Version missing from JSON: %s (%v)", data, err)
	}
}

func TestPromoEdgesStaleDetail(t *testing.T) {
	st := promoRepo(t)
	saveLock(t, st, "dev", store.EnvLock{
		"order-service": {RecordedAt: time.Now().Add(-90 * 24 * time.Hour),
			Provides: map[string]string{"orders.get": blob(t, st, irAB)}},
	})
	saveLock(t, st, "prod", store.EnvLock{
		"web-app": {RecordedAt: time.Now(), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}},
	})
	edges, err := promoEdges(st, []string{"dev", "prod"}, time.Now().Add(-30*24*time.Hour), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.Counterpart == "web-app" {
			if want := "stale deploy record"; !strings.Contains(e.Detail, want) {
				t.Errorf("detail = %q, want it to mention %q", e.Detail, want)
			}
			return
		}
	}
	t.Fatalf("no web-app edge in %+v", edges)
}

func TestCanIDeployFromEnv(t *testing.T) {
	st := promoRepo(t)
	saveLock(t, st, "staging", store.EnvLock{
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{"orders.get": blob(t, st, irB)}},
	})
	saveLock(t, st, "prod", store.EnvLock{
		"web-app": {RecordedAt: time.Now(), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}},
	})
	run := func(args ...string) int {
		return cmdCanIDeploy(append([]string{"--contracts-repo", st.Dir}, args...))
	}
	if code := run("--env", "prod", "--from-env", "staging", "--service", "order-service"); code != 1 {
		t.Errorf("breaking promotion: exit = %d, want 1", code)
	}
	// Promote a version emitting exactly field a: web-app is satisfied and
	// the strict main-branch consumer (mobile) sees nothing unknown.
	saveLock(t, st, "staging", store.EnvLock{
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{"orders.get": blob(t, st, irA)}},
	})
	if code := run("--env", "prod", "--from-env", "staging", "--service", "order-service"); code != 0 {
		t.Errorf("compatible promotion: exit = %d, want 0", code)
	}
	// No record in --from-env: config error, not a verdict.
	if code := run("--env", "prod", "--from-env", "staging", "--service", "nobody"); code != 2 {
		t.Errorf("unrecorded service: exit = %d, want 2", code)
	}
}

func TestServiceInventory(t *testing.T) {
	st := promoRepo(t)
	chash := blob(t, st, irA)
	phash := blob(t, st, irAB)
	saveLock(t, st, "dev", store.EnvLock{
		"order-service": {RecordedAt: time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC), RecordedBy: "erin",
			Provides: map[string]string{"orders.get": phash},
			Consumes: map[string]string{"billing/invoices.get": chash}},
		"phantom": {RecordedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RecordedBy: "gale",
			Provides: map[string]string{"things.list": phash}},
	})
	saveLock(t, st, "prod", store.EnvLock{
		"order-service": {RecordedAt: time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC), RecordedBy: "bob",
			Provides: map[string]string{"orders.get": phash}},
	})
	// The deployed provides hash is the 2nd publish; the consume has no log,
	// so its record falls back to the hash label.
	if err := st.SaveVersions("order-service", store.VersionLog{
		"provides/orders.get": {blob(t, st, irB), phash},
	}); err != nil {
		t.Fatal(err)
	}
	staleBefore := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	inv, err := serviceInventory(st, staleBefore)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != 2 || inv[0].Service != "order-service" || inv[1].Service != "phantom" {
		t.Fatalf("services = %+v, want order-service, phantom", inv)
	}
	os := inv[0]
	if len(os.Envs) != 2 || os.Envs[0].Env != "dev" || os.Envs[1].Env != "prod" {
		t.Fatalf("order-service envs = %+v, want dev, prod", os.Envs)
	}
	dev := os.Envs[0]
	if dev.RecordedAt != "2026-05-02T09:00:00Z" || dev.RecordedBy != "erin" || dev.Stale {
		t.Errorf("dev provenance = %+v", dev)
	}
	if len(dev.Provides) != 1 || dev.Provides[0].Key != "orders.get" || dev.Provides[0].Record.Label() != "v2" {
		t.Errorf("dev provides = %+v, want orders.get v2", dev.Provides)
	}
	if len(dev.Consumes) != 1 || dev.Consumes[0].Key != "billing/invoices.get" ||
		dev.Consumes[0].Record.Label() != shortHash(chash) {
		t.Errorf("dev consumes = %+v, want billing/invoices.get %s", dev.Consumes, shortHash(chash))
	}
	if !inv[1].Envs[0].Stale {
		t.Error("phantom's old record must be stale")
	}
	again, err := serviceInventory(st, staleBefore)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inv, again) {
		t.Error("serviceInventory is not deterministic")
	}
}
