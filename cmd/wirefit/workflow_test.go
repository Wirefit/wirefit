package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wirefit/wirefit/internal/extract"
	"github.com/wirefit/wirefit/internal/manifest"
)

func TestExtractPlan(t *testing.T) {
	m := &manifest.Manifest{
		Provides: []manifest.Interaction{
			{ID: "a", DTO: "com.acme.Order"},
			{ID: "b", Schema: "order.xyz"},                      // schema-only: the artifact IS the contract, still provided
			{ID: "c", DTO: "com.acme.Order", Schema: "o.proto"}, // dto wins; schema stays with the mirror check
		},
		Consumes: []manifest.Consumption{
			{ID: "d", Provider: "billing", DTO: "api.ts#Invoice"},
		},
	}
	targets, specs := extractPlan(m)
	wantSpecs := []extract.Spec{
		{Ref: "com.acme.Order", Role: "provided"},
		{Ref: "order.xyz", Role: "provided"},
		{Ref: "com.acme.Order", Role: "provided"},
		{Ref: "api.ts#Invoice", Role: "consumed"},
	}
	if !reflect.DeepEqual(specs, wantSpecs) {
		t.Errorf("specs = %v, want %v", specs, wantSpecs)
	}
	wantTargets := map[string][]string{
		"com.acme.Order": {filepath.Join("provides", "a.ir.json"), filepath.Join("provides", "c.ir.json")},
		"order.xyz":      {filepath.Join("provides", "b.ir.json")},
		"api.ts#Invoice": {filepath.Join("consumes", "billing", "d.ir.json")},
	}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Errorf("targets = %v, want %v", targets, wantTargets)
	}
}

func TestExternalsMergeByCommand(t *testing.T) {
	reg, wild := externals([]manifest.ExternalExtractor{
		{Match: ".py", Command: "wirefit-py --strict"},
		{Match: ".rb", Command: "wirefit-rb"},
		{Match: ".pyi", Command: "wirefit-py --strict"},
	})
	if len(reg) != 2 || len(wild) != 0 {
		t.Fatalf("len(reg), len(wild) = %d, %d, want 2, 0", len(reg), len(wild))
	}
	py := reg[0].(*extract.External)
	if want := []string{".py", ".pyi"}; !reflect.DeepEqual(py.Suffixes, want) {
		t.Errorf("suffixes = %v, want %v", py.Suffixes, want)
	}
	if want := []string{"wirefit-py", "--strict"}; !reflect.DeepEqual(py.Command, want) {
		t.Errorf("command = %v, want %v", py.Command, want)
	}
	if !py.Match("a.pyi#Order") || py.Match("a.rb") {
		t.Errorf("merged matcher: Match(a.pyi#Order) = %v, Match(a.rb) = %v, want true, false",
			py.Match("a.pyi#Order"), py.Match("a.rb"))
	}
}

func TestExternalsSkipsImporterSuffixes(t *testing.T) {
	reg, wild := externals([]manifest.ExternalExtractor{
		{Match: ".proto", Command: "my-proto-tool"},
	})
	if len(reg) != 0 || len(wild) != 0 {
		t.Errorf("len(reg), len(wild) = %d, %d, want 0, 0: importer-owned suffixes never reach an external", len(reg), len(wild))
	}
}

func TestExternalsWildcardSeparated(t *testing.T) {
	reg, wild := externals([]manifest.ExternalExtractor{
		{Match: ".ts", Command: "wirefit-ts"},
		{Match: "*", Command: "wirefit-java --build-tool gradle"},
	})
	if len(reg) != 1 || len(wild) != 1 {
		t.Fatalf("len(reg), len(wild) = %d, %d, want 1, 1", len(reg), len(wild))
	}
	java := wild[0].(*extract.External)
	if want := []string{"wirefit-java", "--build-tool", "gradle"}; !reflect.DeepEqual(java.Command, want) {
		t.Errorf("command = %v, want %v", java.Command, want)
	}
	if !java.Match("com.acme.Order") || !java.Match("weird.xyz#T") {
		t.Error("wildcard external must match any ref")
	}
}
