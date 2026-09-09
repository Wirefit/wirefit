package tstool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wirefit/wirefit/internal/extrun"
)

func TestCacheEnsureExtractorUsesInstalledDependency(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache root")
	old := extrun.UserCacheDir
	extrun.UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { extrun.UserCacheDir = old })
	t.Setenv("PATH", "")

	dir := filepath.Join(root, "wirefit", "ts-extractor", extractorVersion)
	dependency := filepath.Join(dir, "node_modules", "typescript", "package.json")
	if err := os.MkdirAll(filepath.Dir(dependency), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dependency, []byte(`{"name":"typescript"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureExtractor()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "extract.js"); got != want {
		t.Fatalf("script = %q, want %q", got, want)
	}
	script, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(script) != extractorSource {
		t.Fatal("cached script differs from the embedded extractor")
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Name         string            `json:"name"`
		Private      bool              `json:"private"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "wirefit-ts-extractor" || !pkg.Private || pkg.Dependencies["typescript"] != typescriptVersion {
		t.Fatalf("unexpected package metadata: %+v", pkg)
	}
}
