package extrun

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRunRejectsNonJSONOutput(t *testing.T) {
	_, err := Run("go", func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "oops")
	})
	if err == nil {
		t.Fatal("expected an error for non-JSON extractor output")
	}
	if !strings.Contains(err.Error(), "bad go extractor output") {
		t.Fatalf("expected the unmarshal guard to fire, got: %v", err)
	}
}

func TestRunReturnsSchemaMap(t *testing.T) {
	out, err := Run("go", func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", `{"x#T":{"type":"string"}}`)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out["x#T"]; !ok {
		t.Fatalf("expected key x#T in %v", out)
	}
}
