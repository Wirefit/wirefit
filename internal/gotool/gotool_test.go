package gotool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goModule writes a minimal module so Run gets past the go.mod read (the type
// name guard fires after that, before any code is generated or run).
func goModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunRejectsNonIdentifierTypeName(t *testing.T) {
	dir := goModule(t)
	_, err := Run(dir, []string{`./pkg#Order);os.Exit(0);//`})
	if err == nil {
		t.Fatal("expected an error for a non-identifier type name")
	}
	if !strings.Contains(err.Error(), "must be a Go identifier") {
		t.Fatalf("expected the identifier guard to fire, got: %v", err)
	}
}

func TestRunAcceptsIdentifierTypeName(t *testing.T) {
	dir := goModule(t)
	// A valid identifier clears the guard; Run then fails later building the
	// (absent) package — which proves the guard let it through.
	_, err := Run(dir, []string{`./#Order`})
	if err != nil && strings.Contains(err.Error(), "must be a Go identifier") {
		t.Fatalf("guard wrongly rejected a valid identifier: %v", err)
	}
}
