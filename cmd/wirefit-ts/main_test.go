package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
)

func TestExtractPassesRolesToTstool(t *testing.T) {
	old := runTS
	defer func() { runTS = old }()
	var gotDir string
	var gotProvided, gotConsumed []string
	runTS = func(projectDir string, provided, consumed []string) (map[string]json.RawMessage, error) {
		gotDir = projectDir
		gotProvided = append([]string(nil), provided...)
		gotConsumed = append([]string(nil), consumed...)
		return map[string]json.RawMessage{"ok": json.RawMessage(`{"type":"string"}`)}, nil
	}

	_, err := extract("/svc", []extproto.Spec{
		{Ref: "out.ts#Order", Role: "provided"},
		{Ref: "in.ts#Order", Role: "consumed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != "/svc" {
		t.Fatalf("projectDir = %q", gotDir)
	}
	if !reflect.DeepEqual(gotProvided, []string{"out.ts#Order"}) {
		t.Fatalf("provided = %v", gotProvided)
	}
	if !reflect.DeepEqual(gotConsumed, []string{"in.ts#Order"}) {
		t.Fatalf("consumed = %v", gotConsumed)
	}
}

// The core registry is language-blind, so the both-sides rejection lives here:
// zod io semantics differ per side, so one ts ref cannot serve both. The
// conflict returns before tstool.Run, so no Node toolchain is needed.
func TestExtractRejectsBothRoles(t *testing.T) {
	_, err := extract(".", []extproto.Spec{
		{Ref: "api.ts#Order", Role: "consumed"},
		{Ref: "api.ts#Order", Role: "provided"},
	})
	if err == nil || !strings.Contains(err.Error(), "both provides and consumes") {
		t.Fatalf("want both-roles error, got %v", err)
	}
}
