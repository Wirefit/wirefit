package extserve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
)

// serve runs Serve with request on stdin and returns the decoded response.
func serve(t *testing.T, request string, fn func(string, []extproto.Spec) (map[string]json.RawMessage, error)) (extproto.Response, int) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, []byte(request), 0o644); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	code := Serve(fn)
	os.Stdin, os.Stdout = oldIn, oldOut

	raw, err := os.ReadFile(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	var resp extproto.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("invalid response %q: %v", raw, err)
	}
	return resp, code
}

func TestServeRejectsUnknownRole(t *testing.T) {
	called := false
	resp, code := serve(t, `{"schemaVersion":1,"projectDir":"/svc","specs":[{"ref":"a.py#T","role":"banana"}]}`,
		func(string, []extproto.Spec) (map[string]json.RawMessage, error) {
			called = true
			return nil, nil
		})
	if called {
		t.Error("an unknown role must be rejected before extraction")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0: protocol failures travel in the response body", code)
	}
	if !strings.Contains(resp.Error, "banana") || !strings.Contains(resp.Error, "a.py#T") {
		t.Errorf("error should name the role and the ref: %q", resp.Error)
	}
}

func TestServePassesValidRolesThrough(t *testing.T) {
	var got []extproto.Spec
	resp, code := serve(t, `{"schemaVersion":1,"projectDir":"/svc","specs":[{"ref":"a.py#T","role":"provided"},{"ref":"b.py#U","role":"consumed"}]}`,
		func(dir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
			got = specs
			return map[string]json.RawMessage{"a.py#T": json.RawMessage(`{"x-ct-scalar":"string"}`)}, nil
		})
	if code != 0 || resp.Error != "" {
		t.Fatalf("code = %d, error = %q", code, resp.Error)
	}
	if len(got) != 2 {
		t.Fatalf("specs = %v, want both", got)
	}
	if _, ok := resp.Schemas["a.py#T"]; !ok {
		t.Errorf("schemas = %v, want a.py#T", resp.Schemas)
	}
}
