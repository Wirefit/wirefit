package main

import (
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
)

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
