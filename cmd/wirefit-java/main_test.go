package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/javatool"
)

func TestParseFlags(t *testing.T) {
	t.Setenv("WIREFIT_EXTRACTOR_CP", "/cache/wirefit.jar")
	opts, code := parse([]string{"--build-tool", "gradle", "--mapper", "com.acme.Json#mapper", "--java", "/opt/jdk/bin/java"})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := javatool.RunOptions{
		BuildTool:   "gradle",
		ExtractorCP: "/cache/wirefit.jar",
		Mapper:      "com.acme.Json#mapper",
		JavaBin:     "/opt/jdk/bin/java",
	}
	if opts != want {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	if _, code := parse([]string{"--no-such-flag"}); code != 2 {
		t.Fatalf("code = %d", code)
	}
}

// Jackson is role-agnostic, so a ref used on both sides must reach javatool
// once: extract dedupes and sorts via extserve.Refs.
func TestExtractDedupesRoles(t *testing.T) {
	old := runJava
	defer func() { runJava = old }()
	var gotOpts javatool.RunOptions
	var gotFQNs []string
	runJava = func(opts javatool.RunOptions, fqns []string) (map[string]json.RawMessage, error) {
		gotOpts = opts
		gotFQNs = append([]string(nil), fqns...)
		return map[string]json.RawMessage{"ok": json.RawMessage(`{"type":"string"}`)}, nil
	}

	_, err := extract(javatool.RunOptions{BuildTool: "maven"}, "/svc", []extproto.Spec{
		{Ref: "com.acme.Order", Role: "provided"},
		{Ref: "com.acme.Order", Role: "consumed"},
		{Ref: "com.acme.Item", Role: "consumed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOpts.ProjectDir != "/svc" || gotOpts.BuildTool != "maven" {
		t.Fatalf("opts = %+v", gotOpts)
	}
	if !reflect.DeepEqual(gotFQNs, []string{"com.acme.Item", "com.acme.Order"}) {
		t.Fatalf("fqns = %v", gotFQNs)
	}
}
