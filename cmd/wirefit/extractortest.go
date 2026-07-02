package main

// wirefit extractor-test (PRD 3.2): conformance kit for third-party
// extractors. Runs the given executable via the public protocol against the
// author's own fixtures and compares canonical IR hashes with the embedded
// corpus expectations — a pass means the extractor agrees byte-for-byte with
// every other wirefit extractor.

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/wirefit/wirefit/internal/confexpected"
	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/ir"
)

type extCase struct {
	Name string `yaml:"name"` // corpus case name (Scalars, Presence, ...)
	Spec string `yaml:"spec"` // the extractor's own fixture reference
	Role string `yaml:"role"` // provided | consumed (default consumed)
}

type extCasesFile struct {
	Cases []extCase `yaml:"cases"`
}

func cmdExtractorTest(args []string) int {
	fs := flag.NewFlagSet("extractor-test", flag.ContinueOnError)
	casesFile := fs.String("cases", "cases.yaml", "case mapping file (corpus name → fixture spec)")
	projectDir := fs.String("project", ".", "fixture project directory")
	if fs.Parse(args) != nil {
		return 2
	}
	command := fs.Args()
	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "usage: wirefit extractor-test --cases cases.yaml --project dir -- <extractor-command...>")
		return 2
	}
	data, err := os.ReadFile(*casesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit extractor-test:", err)
		return 2
	}
	var cf extCasesFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		fmt.Fprintln(os.Stderr, "wirefit extractor-test:", err)
		return 2
	}
	if len(cf.Cases) == 0 {
		fmt.Fprintln(os.Stderr, "wirefit extractor-test: no cases declared")
		return 2
	}

	req := extproto.Request{ProjectDir: *projectDir}
	covered := map[string]bool{}
	for _, c := range cf.Cases {
		if _, err := confexpected.Get(c.Name); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extractor-test:", err)
			return 2
		}
		role := c.Role
		if role == "" {
			role = "consumed"
		}
		req.Specs = append(req.Specs, extproto.Spec{Ref: c.Spec, Role: role})
		covered[c.Name] = true
	}

	resp, err := extproto.Invoke(command, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit extractor-test:", err)
		return 1
	}

	fail := false
	for _, c := range cf.Cases {
		raw, ok := resp.Schemas[c.Spec]
		if !ok {
			fmt.Printf("FAIL %-12s no schema returned for %s\n", c.Name, c.Spec)
			fail = true
			continue
		}
		got, err := ir.Parse(raw)
		if err != nil {
			fmt.Printf("FAIL %-12s invalid IR: %v\n", c.Name, err)
			fail = true
			continue
		}
		gotHash, _ := ir.HashSchema(got)
		expRaw, _ := confexpected.Get(c.Name)
		wantHash, _ := ir.Hash(expRaw)
		if gotHash == wantHash {
			fmt.Printf("OK   %-12s %s\n", c.Name, gotHash)
		} else {
			fmt.Printf("FAIL %-12s got %s want %s\n", c.Name, gotHash, wantHash)
			fail = true
		}
	}
	var uncovered []string
	for _, name := range confexpected.Names() {
		if !covered[name] {
			uncovered = append(uncovered, name)
		}
	}
	if len(uncovered) > 0 {
		fmt.Printf("note: corpus cases not covered by this extractor: %v\n", uncovered)
	}
	if fail {
		return 1
	}
	fmt.Println("extractor-test: PASS, extractor agrees with the wirefit corpus")
	return 0
}
