package override

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
)

var now = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wirefit-overrides.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const valid = `
overrides:
  - interaction: orders.get-order
    path: $.customer_email
    rule: field-removed
    downgrade-to: warning
    justification: coordinated removal, JIRA-123, web-app deploys first
    expires: "2026-08-01"
`

func TestApplyDowngradesAndTags(t *testing.T) {
	f, errs := Load(write(t, valid), now)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	r := &diff.Result{Findings: []diff.Finding{
		{Class: diff.Breaking, Rule: "field-removed", Path: "$.customer_email", Message: "field removed"},
		{Class: diff.Breaking, Rule: "field-removed", Path: "$.other", Message: "field removed"},
	}}
	applied := f.Apply("orders.get-order", r)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
	if r.Findings[0].Class != diff.Warning || !r.Findings[0].Overridden {
		t.Errorf("finding not downgraded/tagged: %+v", r.Findings[0])
	}
	if r.Findings[1].Class != diff.Breaking {
		t.Error("unrelated finding must stay breaking")
	}
	if r.ExitCode() != 1 {
		t.Error("other breaking finding still gates")
	}
	if len(f.Stale()) != 0 {
		t.Error("applied override must not be stale")
	}
}

func TestWrongInteractionIsStale(t *testing.T) {
	f, errs := Load(write(t, valid), now)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	r := &diff.Result{Findings: []diff.Finding{
		{Class: diff.Breaking, Rule: "field-removed", Path: "$.customer_email"},
	}}
	f.Apply("billing.invoice", r)
	if r.Findings[0].Class != diff.Breaking {
		t.Error("override for another interaction must not apply")
	}
	if len(f.Stale()) != 1 {
		t.Error("unused override must be reported stale")
	}
}

func TestValidationFailures(t *testing.T) {
	cases := map[string]string{
		"expired": `
overrides:
  - {interaction: a.b, path: $.x, rule: field-removed, downgrade-to: safe, justification: j, expires: "2026-06-01"}`,
		"too far out": `
overrides:
  - {interaction: a.b, path: $.x, rule: field-removed, downgrade-to: safe, justification: j, expires: "2027-06-09"}`,
		"missing justification": `
overrides:
  - {interaction: a.b, path: $.x, rule: field-removed, downgrade-to: safe, expires: "2026-08-01"}`,
		"bad downgrade": `
overrides:
  - {interaction: a.b, path: $.x, rule: field-removed, downgrade-to: neutral, justification: j, expires: "2026-08-01"}`,
		"unknown key": `
overrides:
  - {interaction: a.b, path: $.x, rule: field-removed, downgrade-to: safe, justification: j, expires: "2026-08-01", commment: typo}`,
	}
	for name, content := range cases {
		if _, errs := Load(write(t, content), now); len(errs) == 0 {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	f, errs := Load(filepath.Join(t.TempDir(), "nope.yaml"), now)
	if len(errs) != 0 || len(f.Overrides) != 0 {
		t.Fatal("missing overrides file must be empty, not an error")
	}
}
