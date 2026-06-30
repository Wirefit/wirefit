package javatool

import (
	"strings"
	"testing"
)

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
