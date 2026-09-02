package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/diff"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMissingFileIsEmptyPolicy(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rules) != 0 {
		t.Errorf("want empty policy, got %v", p.Rules)
	}
	if !p.Overridable("field-removed") {
		t.Error("everything is overridable by default")
	}
}

func TestReclassifyAndForbidOverride(t *testing.T) {
	dir := write(t, "rules:\n  field-removed:\n    class: warning\n    overridable: false\n")
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := &diff.Result{Findings: []diff.Finding{
		{Class: diff.Breaking, Rule: "field-removed", Path: "$.email", Message: "field removed"},
	}}
	p.Apply(r)
	if r.Findings[0].Class != diff.Warning {
		t.Errorf("class = %v, want warning", r.Findings[0].Class)
	}
	if !strings.Contains(r.Findings[0].Message, "org policy") {
		t.Errorf("reclassification should be visible in the message: %q", r.Findings[0].Message)
	}
	if p.Overridable("field-removed") {
		t.Error("overridable: false must forbid per-service overrides")
	}
}

func TestUnknownClassRejected(t *testing.T) {
	_, err := Load(write(t, "rules:\n  field-removed:\n    class: catastrophic\n"))
	if err == nil || !strings.Contains(err.Error(), "catastrophic") {
		t.Fatalf("want error naming the bad class, got %v", err)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	_, err := Load(write(t, "ruels:\n  field-removed:\n    class: warning\n"))
	if err == nil || !strings.Contains(err.Error(), "ruels") {
		t.Fatalf("want error naming the unknown key, got %v", err)
	}
}

// A second document would otherwise be dropped in silence, so a policy that
// looks stricter than it is could gate deployments the wrong way.
func TestSecondDocumentRejected(t *testing.T) {
	content := "rules:\n  field-removed:\n    class: warning\n---\nrules:\n  field-added:\n    class: safe\n"
	_, err := Load(write(t, content))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("want multi-document error, got %v", err)
	}
}
