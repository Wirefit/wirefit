// wirefit — language-agnostic contract checking CLI (Phase 1).
//
// Exit codes (PRD 1.7): 0 ok/warnings · 1 breaking changes · 2 config/input error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/manifest"
)

const version = "0.1.0-dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "validate":
		return cmdValidate(args[1:])
	case "hash":
		return cmdHash(args[1:])
	case "diff":
		return cmdDiff(args[1:])
	case "compat":
		return cmdCompat(args[1:])
	case "version":
		fmt.Println("wirefit " + version)
		return 0
	case "extract":
		return cmdExtract(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "publish":
		return cmdPublish(args[1:])
	case "extractor-test":
		return cmdExtractorTest(args[1:])
	case "record-deploy":
		return cmdRecordDeploy(args[1:])
	case "can-i-deploy":
		return cmdCanIDeploy(args[1:])
	case "matrix":
		return cmdMatrix(args[1:])
	case "override":
		if len(args) > 1 && args[1] == "add" {
			return cmdOverrideAdd(args[2:])
		}
		fmt.Fprintln(os.Stderr, "usage: wirefit override add [flags]")
		return 2
	case "init":
		return cmdInit(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "wirefit: unknown command %q\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `wirefit: language-agnostic contract checking

usage: wirefit <command> [flags]

  validate   validate a contracts.yaml manifest
  extract    extract IR from DTOs declared in the manifest
  check      check candidate IR against the contracts repo (PR gate)
  publish    publish extracted IR to the contracts repo (merge to main)
  hash       print the canonical content hash of an IR file
  diff       low-level before/after self-diff of two IR files
  compat     low-level producer-vs-consumer compatibility check
  extractor-test  conformance kit for third-party extractors (docs/extractor-protocol.md)
  override add    append a justified, expiring override to wirefit-overrides.yaml
  record-deploy   pin this service's published contracts as deployed in an env
  can-i-deploy    check the candidate against what is DEPLOYED in an env
                  (--from-env <env>: gate promoting what runs there instead)
  matrix          render the deployed compatibility matrix across envs, plus
                  promotion readiness when _envs/pipeline.yaml (or --envs) orders them
  version    print version

not yet implemented: init
`)
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	file := fs.String("f", "contracts.yaml", "manifest file")
	if fs.Parse(args) != nil {
		return 2
	}
	m, err := manifest.Load(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit validate:", err)
		return 2
	}
	if errs := m.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "wirefit validate:", e)
		}
		return 2
	}
	fmt.Printf("manifest OK: service %s, %d provided, %d consumed\n",
		m.Service, len(m.Provides), len(m.Consumes))
	return 0
}

func cmdHash(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: wirefit hash <ir-file.json>")
		return 2
	}
	s, err := ir.Load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit hash:", err)
		return 2
	}
	h, err := ir.HashSchema(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit hash:", err)
		return 2
	}
	fmt.Println(h)
	return 0
}

// consumersFile format:
//
//	{"web-app": {"schema": { ...IR... }, "rejectUnknown": true}, ...}
func loadConsumers(path string) (map[string]diff.Consumer, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Schema        json.RawMessage `json:"schema"`
		RejectUnknown bool            `json:"rejectUnknown"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("consumers file: %w", err)
	}
	out := map[string]diff.Consumer{}
	for name, e := range raw {
		s, err := ir.Parse(e.Schema)
		if err != nil {
			return nil, fmt.Errorf("consumers file, %q: %w", name, err)
		}
		out[name] = diff.Consumer{Schema: s, RejectUnknown: e.RejectUnknown}
	}
	return out, nil
}

func cmdDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	before := fs.String("before", "", "IR file: previously published schema")
	after := fs.String("after", "", "IR file: candidate schema")
	dirFlag := fs.String("direction", "response", "response|event|request (or p2c|c2p)")
	consumersFile := fs.String("consumers", "", "JSON file with registered consumer projections")
	providerReject := fs.Bool("provider-rejects-unknown", false, "provider deserializer rejects unknown fields (c2p)")
	format := fs.String("format", "text", "text|json")
	if fs.Parse(args) != nil {
		return 2
	}
	if *before == "" || *after == "" {
		fmt.Fprintln(os.Stderr, "wirefit diff: -before and -after are required")
		return 2
	}
	dir, err := diff.ParseDirection(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit diff:", err)
		return 2
	}
	b, err := ir.Load(*before)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit diff: before:", err)
		return 2
	}
	a, err := ir.Load(*after)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit diff: after:", err)
		return 2
	}
	consumers, err := loadConsumers(*consumersFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit diff:", err)
		return 2
	}
	r := diff.Self(b, a, diff.SelfOptions{
		Direction:              dir,
		Consumers:              consumers,
		ProviderRejectsUnknown: *providerReject,
		ColdStart:              len(consumers) == 0,
	})
	printResult(r, *format)
	return r.ExitCode()
}

func cmdCompat(args []string) int {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	provider := fs.String("provider", "", "IR file: provider schema")
	consumer := fs.String("consumer", "", "IR file: consumer schema")
	dirFlag := fs.String("direction", "response", "response|event|request (or p2c|c2p)")
	strict := fs.Bool("strict-parser", false, "parsing side rejects unknown fields")
	format := fs.String("format", "text", "text|json")
	if fs.Parse(args) != nil {
		return 2
	}
	if *provider == "" || *consumer == "" {
		fmt.Fprintln(os.Stderr, "wirefit compat: -provider and -consumer are required")
		return 2
	}
	dir, err := diff.ParseDirection(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit compat:", err)
		return 2
	}
	p, err := ir.Load(*provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit compat: provider:", err)
		return 2
	}
	c, err := ir.Load(*consumer)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit compat: consumer:", err)
		return 2
	}
	r := diff.Compat(p, c, diff.CompatOptions{Direction: dir, StrictParser: *strict})
	printResult(r, *format)
	return r.ExitCode()
}
