package extserve

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
)

func TestSplitRoles(t *testing.T) {
	provided, consumed, err := SplitRoles([]extproto.Spec{
		{Ref: "out.ts#Order", Role: "provided"},
		{Ref: "in.ts#Order", Role: "consumed"},
		{Ref: "in.ts#Item", Role: "consumed"},
	}, "zod io semantics differ per side")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provided, []string{"out.ts#Order"}) {
		t.Fatalf("provided = %v", provided)
	}
	if !reflect.DeepEqual(consumed, []string{"in.ts#Order", "in.ts#Item"}) {
		t.Fatalf("consumed = %v", consumed)
	}
}

func TestSplitRolesRejectsBothRoles(t *testing.T) {
	_, _, err := SplitRoles([]extproto.Spec{
		{Ref: "api.ts#Order", Role: "consumed"},
		{Ref: "api.ts#Order", Role: "provided"},
	}, "zod io semantics differ per side")
	if err == nil || !strings.Contains(err.Error(), "both provides and consumes") ||
		!strings.Contains(err.Error(), "zod io semantics differ per side") {
		t.Fatalf("want both-roles error carrying the why, got %v", err)
	}
}

func TestRefs(t *testing.T) {
	got := Refs([]extproto.Spec{
		{Ref: "com.acme.Order", Role: "provided"},
		{Ref: "com.acme.Order", Role: "consumed"},
		{Ref: "com.acme.Item", Role: "consumed"},
	})
	if !reflect.DeepEqual(got, []string{"com.acme.Item", "com.acme.Order"}) {
		t.Fatalf("refs = %v", got)
	}
}
