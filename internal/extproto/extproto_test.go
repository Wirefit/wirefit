package extproto

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const helperEnv = "WIREFIT_EXTPROTO_HELPER"

// TestHelperExtractor is not a test: re-executing this binary is a portable
// stand-in for a third-party extractor executable (a shell script would not
// run on Windows). Args for the fake extractor follow "--".
func TestHelperExtractor(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	if mode == "malformed" {
		fmt.Println("this is not JSON")
		os.Exit(0)
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	var req Request
	if err := json.Unmarshal(in, &req); err != nil {
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	probe, err := json.Marshal(map[string]any{"cwd": cwd, "projectDir": req.ProjectDir, "args": args})
	if err != nil {
		os.Exit(1)
	}
	resp := Response{SchemaVersion: SchemaVersion, Schemas: map[string]json.RawMessage{}}
	if mode != "partial" {
		for _, s := range req.Specs {
			resp.Schemas[s.Ref] = probe
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

type probe struct {
	CWD        string   `json:"cwd"`
	ProjectDir string   `json:"projectDir"`
	Args       []string `json:"args"`
}

func helper(t *testing.T, mode string, args ...string) []string {
	t.Helper()
	t.Setenv(helperEnv, mode)
	return append([]string{os.Args[0], "-test.run=TestHelperExtractor", "--"}, args...)
}

func invokeProbe(t *testing.T, command []string, projectDir string) probe {
	t.Helper()
	resp, err := Invoke(command, Request{ProjectDir: projectDir, Specs: []Spec{{Ref: "a.py#T", Role: RoleConsumed}}})
	if err != nil {
		t.Fatal(err)
	}
	var p probe
	if err := json.Unmarshal(resp.Schemas["a.py#T"], &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// The extractor's working directory IS the service project, and projectDir
// arrives absolute — the two guarantees docs/extractor-protocol.md makes.
func TestInvokeRunsInAbsoluteProjectDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "service")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	p := invokeProbe(t, helper(t, "ok"), "service")

	if !filepath.IsAbs(p.ProjectDir) {
		t.Errorf("projectDir = %q, want an absolute path", p.ProjectDir)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(p.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("projectDir = %q, want %q", got, want)
	}
	if got, err := filepath.EvalSymlinks(p.CWD); err != nil || got != want {
		t.Errorf("extractor cwd = %q (%v), want %q", p.CWD, err, want)
	}
}

// argv, not a shell string: an argument containing spaces reaches the
// extractor whole.
func TestInvokePassesArgumentsWithSpaces(t *testing.T) {
	p := invokeProbe(t, helper(t, "ok", "--classpath", "/opt/my libs/a.jar", "--flag"), t.TempDir())
	want := []string{"--classpath", "/opt/my libs/a.jar", "--flag"}
	if len(p.Args) != len(want) {
		t.Fatalf("args = %q, want %q", p.Args, want)
	}
	for i := range want {
		if p.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", p.Args, want)
		}
	}
}

func TestInvokeRejectsInvalidRole(t *testing.T) {
	_, err := Invoke([]string{"never-spawned"}, Request{Specs: []Spec{{Ref: "a.py#T", Role: "banana"}}})
	if err == nil {
		t.Fatal("an unknown role must fail before the extractor runs")
	}
	if !strings.Contains(err.Error(), "banana") || !strings.Contains(err.Error(), "a.py#T") {
		t.Errorf("error should name the role and the ref: %v", err)
	}
}

func TestInvokeRejectsMalformedOutput(t *testing.T) {
	_, err := Invoke(helper(t, "malformed"), Request{ProjectDir: t.TempDir(), Specs: []Spec{{Ref: "a.py#T", Role: RoleProvided}}})
	if err == nil {
		t.Fatal("non-JSON output must fail")
	}
	if !strings.Contains(err.Error(), "invalid response JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// A response that simply omits a requested ref is not a protocol error: the
// caller reports the gap per ref (extract names the dto, extractor-test the
// case), which beats one opaque failure for the whole batch.
func TestInvokeReportsMissingSchemasToCaller(t *testing.T) {
	resp, err := Invoke(helper(t, "partial"), Request{
		ProjectDir: t.TempDir(),
		Specs:      []Spec{{Ref: "a.py#T", Role: RoleConsumed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.Schemas["a.py#T"]; ok {
		t.Error("helper returned no schemas; Schemas must be empty")
	}
}

func TestInvokeRejectsEmptyCommand(t *testing.T) {
	if _, err := Invoke(nil, Request{}); err == nil {
		t.Fatal("an empty command must fail")
	}
}
