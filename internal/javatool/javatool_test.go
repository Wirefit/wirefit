package javatool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extrun"
)

func TestCacheDir(t *testing.T) {
	root := t.TempDir()
	old := extrun.UserCacheDir
	extrun.UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { extrun.UserCacheDir = old })

	got, err := cacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "wirefit", "java-extractor", extractorVersion)
	if got != want {
		t.Fatalf("cache = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("cache %q is not a directory", got)
	}
}

// Run with no classpath and --build-tool none must fail before ever shelling out
// to java: it proves the option threading reaches ResolveClasspath and that the
// Java path is now exercisable without the CLI command.
func TestRunBuildToolNoneRequiresClasspath(t *testing.T) {
	_, err := Run(RunOptions{ProjectDir: t.TempDir(), BuildTool: "none"}, []string{"com.example.Order"})
	if err == nil {
		t.Fatal("expected an error when --build-tool none has no explicit classpath")
	}
	if !strings.Contains(err.Error(), "requires an explicit --classpath") {
		t.Fatalf("expected the classpath guard to fire, got: %v", err)
	}
}
