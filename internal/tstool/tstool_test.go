package tstool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extrun"
)

func TestCacheEnsureExtractorUsesInstalledDependency(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache root")
	old := extrun.UserCacheDir
	extrun.UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { extrun.UserCacheDir = old })
	t.Setenv("PATH", "")

	pkg := fmt.Sprintf(`{"name":"wirefit-ts-extractor","private":true,"dependencies":{"typescript":"%s"}}`, typescriptVersion)
	dir := extrun.CachePath(filepath.Join(root, "wirefit", "ts-extractor", extractorVersion), extractorSource, pkg)
	if err := extrun.EnsureDir(dir, func(work string) error {
		dependency := filepath.Join(work, "node_modules", "typescript", "package.json")
		if err := os.MkdirAll(filepath.Dir(dependency), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dependency, []byte(`{"name":"typescript"}`), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(work, "package.json"), []byte(pkg), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(work, "extract.js"), []byte(extractorSource), 0o644)
	}); err != nil {
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
	var metadata struct {
		Name         string            `json:"name"`
		Private      bool              `json:"private"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "wirefit-ts-extractor" || !metadata.Private || metadata.Dependencies["typescript"] != typescriptVersion {
		t.Fatalf("unexpected package metadata: %+v", metadata)
	}
}

func TestCacheInstallsOncePerSource(t *testing.T) {
	log := fakeNPM(t)
	source := extractorSource
	t.Cleanup(func() { extractorSource = source })
	a, err := EnsureExtractor()
	if err != nil {
		t.Fatal(err)
	}
	if again, err := EnsureExtractor(); err != nil || again != a {
		t.Fatalf("repeat cache = %q, %v", again, err)
	}
	extractorSource += "\n// different extractor\n"
	b, err := EnsureExtractor()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different sources share a script path")
	}
	if data, err := os.ReadFile(a); err != nil || string(data) != source {
		t.Fatalf("existing script changed: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	work := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(work) != 2 || work[0] == work[1] || work[0] == filepath.Dir(a) || work[1] == filepath.Dir(b) {
		t.Fatalf("installations did not use isolated staging: %q", data)
	}
	for _, dir := range work {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("staging directory survived: %s, %v", dir, err)
		}
	}
}

func TestCacheFailedInstallCanRetry(t *testing.T) {
	fakeNPM(t)
	t.Setenv("WIREFIT_TEST_INSTALL_FAIL", "1")
	if _, err := EnsureExtractor(); err == nil {
		t.Fatal("failed npm install was accepted")
	}
	dir, err := cacheDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed installation left a cache: %v, %v", entries, err)
	}
	t.Setenv("WIREFIT_TEST_INSTALL_FAIL", "")
	if _, err := EnsureExtractor(); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func fakeNPM(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake npm is unix-only")
	}
	root := t.TempDir()
	old := extrun.UserCacheDir
	extrun.UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { extrun.UserCacheDir = old })
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "installations")
	t.Setenv("WIREFIT_TEST_INSTALL_LOG", log)
	script := `#!/bin/sh
set -eu
pwd >> "$WIREFIT_TEST_INSTALL_LOG"
if [ "${WIREFIT_TEST_INSTALL_FAIL:-}" = 1 ]; then exit 1; fi
mkdir -p node_modules/typescript
printf '{"name":"typescript"}' > node_modules/typescript/package.json
`
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}
