package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/pytool"
)

func TestParsePythonFlag(t *testing.T) {
	opts, code := parse([]string{"--python", ".venv/bin/python"})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if opts.python != ".venv/bin/python" {
		t.Fatalf("python = %q", opts.python)
	}
}

func TestExtractPassesRolesToPytool(t *testing.T) {
	old := runPython
	defer func() { runPython = old }()
	var gotOpts pytool.RunOptions
	var gotProvided, gotConsumed []string
	runPython = func(opts pytool.RunOptions, provided, consumed []string) (map[string]json.RawMessage, error) {
		gotOpts = opts
		gotProvided = append([]string(nil), provided...)
		gotConsumed = append([]string(nil), consumed...)
		return map[string]json.RawMessage{"ok": json.RawMessage(`{"type":"string"}`)}, nil
	}

	_, err := extract(options{python: ".venv/bin/python"}, "/svc", []extproto.Spec{
		{Ref: "out.py#Order", Role: "provided"},
		{Ref: "in.py#Order", Role: "consumed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOpts.ProjectDir != "/svc" || gotOpts.PythonBin != ".venv/bin/python" {
		t.Fatalf("opts = %+v", gotOpts)
	}
	if !reflect.DeepEqual(gotProvided, []string{"out.py#Order"}) {
		t.Fatalf("provided = %v", gotProvided)
	}
	if !reflect.DeepEqual(gotConsumed, []string{"in.py#Order"}) {
		t.Fatalf("consumed = %v", gotConsumed)
	}
}

func TestExtractRejectsBothRoles(t *testing.T) {
	_, err := extract(options{python: "python3"}, ".", []extproto.Spec{
		{Ref: "api.py#Order", Role: "consumed"},
		{Ref: "api.py#Order", Role: "provided"},
	})
	if err == nil || !strings.Contains(err.Error(), "pydantic io semantics differ per side") {
		t.Fatalf("err = %v", err)
	}
}
