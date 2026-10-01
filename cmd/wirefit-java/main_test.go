package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/javatool"
)

func TestParseFlags(t *testing.T) {
	opts, code := parse([]string{"--build-tool", "gradle", "--mapper", "com.acme.Json#mapper", "--java", "/opt/jdk/bin/java"})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if opts.BuildTool != "gradle" || opts.Mapper != "com.acme.Json#mapper" || opts.JavaBin != "/opt/jdk/bin/java" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	if _, code := parse([]string{"--nope"}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

// Jackson draws no io distinction: a ref used on both sides extracts once,
// and the refs reach javatool sorted (NF3).
func TestExtractDedupsRefsAcrossRoles(t *testing.T) {
	old := runJava
	defer func() { runJava = old }()
	var gotOpts javatool.RunOptions
	var gotRefs []string
	runJava = func(opts javatool.RunOptions, fqns []string) (map[string]json.RawMessage, error) {
		gotOpts = opts
		gotRefs = append([]string(nil), fqns...)
		return map[string]json.RawMessage{}, nil
	}

	_, err := extract(javatool.RunOptions{BuildTool: "maven"}, "/svc", []extproto.Spec{
		{Ref: "com.acme.Order", Role: "provided"},
		{Ref: "com.acme.Invoice", Role: "consumed"},
		{Ref: "com.acme.Order", Role: "consumed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOpts.ProjectDir != "/svc" || gotOpts.BuildTool != "maven" {
		t.Fatalf("opts = %+v", gotOpts)
	}
	if want := []string{"com.acme.Invoice", "com.acme.Order"}; !reflect.DeepEqual(gotRefs, want) {
		t.Fatalf("refs = %v, want %v", gotRefs, want)
	}
}
