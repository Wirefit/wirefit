package main

// extract / check / publish: the manifest-driven workflow commands (PRD 1.7).
//
// Local IR layout produced by extract and consumed by check/publish:
//
//	<ir-dir>/provides/<interaction-id>.ir.json
//	<ir-dir>/consumes/<provider>/<interaction-id>.ir.json

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/extract"
	"github.com/wirefit/wirefit/internal/gotool"
	"github.com/wirefit/wirefit/internal/importer"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/manifest"
	"github.com/wirefit/wirefit/internal/override"
	"github.com/wirefit/wirefit/internal/policy"
	"github.com/wirefit/wirefit/internal/store"
)

func loadManifest(path string) (*manifest.Manifest, int) {
	m, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit:", err)
		return nil, 2
	}
	if errs := m.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "wirefit: manifest:", e)
		}
		return nil, 2
	}
	return m, 0
}

// ---------------------------------------------------------------- extract --

func cmdExtract(args []string) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	mf := fs.String("f", "contracts.yaml", "manifest file")
	projectDir := fs.String("project", ".", "service project directory")
	irDir := fs.String("ir", ".wirefit/ir", "output directory for extracted IR")
	if fs.Parse(args) != nil {
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}
	if m.Settings.JavaMapper != "" {
		fmt.Fprintln(os.Stderr, "wirefit extract: warning: settings.java-mapper is unused; pass --mapper on the wirefit-java extractor command instead")
	}

	targets, specs := extractPlan(m)

	// Routing per DTO reference (PRD 2.1) lives in the extractor registry:
	// each extractor owns its Match rule, first match in registry order wins.
	// Suffix-routed externals (PRD 3.2) outrank gotool; the wildcard external
	// (java FQNs have no syntactic marker) goes last so it cannot swallow
	// "./pkg#Type" refs. Roles matter to extractors whose source
	// distinguishes input/output semantics, e.g. Zod `.default()` (PRD 2.3).
	suffixExt, wildcardExt := externals(m.Extractors)
	reg := []extract.Extractor{
		// Schema-native formats stay with the importer even when a manifest
		// extractor claims their suffix; externals warns about the shadowing.
		importer.Extractor{Opts: importer.Options{GraphQLSchema: m.Settings.GraphQLSchema}},
	}
	reg = append(reg, suffixExt...)
	reg = append(reg, gotool.Extractor{})
	reg = append(reg, wildcardExt...)
	extracted, err := extract.Run(reg, *projectDir, specs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit extract:", err)
		return 2
	}
	// Mirror check (PRD 5.7): when an interaction declares BOTH code dto and
	// schema artifact, they must agree byte-for-byte. Drift always fails —
	// no override (a schema file lying about the code is never acceptable).
	for _, p := range m.Provides {
		if p.DTO == "" || p.Schema == "" {
			continue
		}
		schemaRaw, err := importer.Import(*projectDir, p.Schema, importer.Options{GraphQLSchema: m.Settings.GraphQLSchema})
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract: mirror:", err)
			return 2
		}
		schemaIR, err := ir.Parse(schemaRaw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit extract: mirror %s: %v\n", p.Schema, err)
			return 2
		}
		dtoIR, err := ir.Parse(extracted[p.DTO])
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit extract: mirror %s: %v\n", p.DTO, err)
			return 2
		}
		hs, _ := ir.HashSchema(schemaIR)
		hd, _ := ir.HashSchema(dtoIR)
		if hs != hd {
			fmt.Fprintf(os.Stderr, "wirefit extract: MIRROR DRIFT on %s: %s and %s disagree:\n", p.ID, p.Schema, p.DTO)
			dir, _ := diff.ParseDirection(p.Direction)
			r := diff.Self(schemaIR, dtoIR, diff.SelfOptions{Direction: dir})
			for _, f := range r.Findings {
				fmt.Fprintf(os.Stderr, "  %s %s %s\n", f.Rule, f.Path, f.Message)
			}
			return 1
		}
	}

	for fqn, rels := range targets {
		raw, ok := extracted[fqn]
		if !ok {
			fmt.Fprintf(os.Stderr, "wirefit extract: extractor returned nothing for %s\n", fqn)
			return 2
		}
		if _, err := ir.Parse(raw); err != nil {
			fmt.Fprintf(os.Stderr, "wirefit extract: %s: %v\n", fqn, err)
			return 2
		}
		canon, err := ir.Canonicalize(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit extract: %s: %v\n", fqn, err)
			return 2
		}
		pretty, perr := ir.CanonicalIndent(canon)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "wirefit extract: %s: %v\n", fqn, perr)
			return 2
		}
		for _, rel := range rels {
			p := filepath.Join(*irDir, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
			if err := os.WriteFile(p, append(pretty, '\n'), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
		}
	}
	fmt.Printf("extracted %s into %s\n", plural(len(targets), "schema"), *irDir)
	return 0
}

// extractPlan maps manifest interactions to extraction inputs: dto → relative
// IR output paths (the same DTO may back several interactions) plus the
// (ref, role) spec list, which extract.Run dedups. A schema-only provides
// extracts the schema artifact itself, still as provided (Phase 5).
func extractPlan(m *manifest.Manifest) (targets map[string][]string, specs []extract.Spec) {
	targets = map[string][]string{}
	for _, p := range m.Provides {
		src := p.DTO
		if src == "" {
			src = p.Schema // schema-native artifact IS the contract (Phase 5)
		}
		targets[src] = append(targets[src], filepath.Join("provides", p.ID+".ir.json"))
		specs = append(specs, extract.Spec{Ref: src, Role: "provided"})
	}
	for _, c := range m.Consumes {
		targets[c.DTO] = append(targets[c.DTO], filepath.Join("consumes", c.Provider, c.ID+".ir.json"))
		specs = append(specs, extract.Spec{Ref: c.DTO, Role: "consumed"})
	}
	return targets, specs
}

// externals merges manifest extractor entries sharing a command into one
// registry entry, so the command is spawned once with its full spec set.
// The wildcard fallback ("*", suffix-less refs like java FQNs) comes back
// separately: it registers after gotool, suffix rules before it. Entries for
// suffixes the built-in importer owns can never fire (the importer is
// registered ahead of them); warn instead of failing silently.
func externals(entries []manifest.ExternalExtractor) (suffix, wildcard []extract.Extractor) {
	byCmd := map[string]*extract.External{}
	for _, x := range entries {
		if x.Match == "*" {
			wildcard = append(wildcard, &extract.External{Suffixes: []string{"*"}, Command: strings.Fields(x.Command)})
			continue
		}
		if importer.IsSpec(x.Match) {
			fmt.Fprintf(os.Stderr, "wirefit extract: warning: extractor for %s never runs; the built-in importer handles that format\n", x.Match)
			continue
		}
		cmd := strings.Fields(x.Command)
		key := strings.Join(cmd, "\x00")
		if e := byCmd[key]; e != nil {
			e.Suffixes = append(e.Suffixes, x.Match)
			continue
		}
		e := &extract.External{Suffixes: []string{x.Match}, Command: cmd}
		byCmd[key] = e
		suffix = append(suffix, e)
	}
	return suffix, wildcard
}

// ------------------------------------------------------------------ check --

func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	mf := fs.String("f", "contracts.yaml", "manifest file")
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	irDir := fs.String("ir", ".wirefit/ir", "directory with extracted candidate IR")
	format := fs.String("format", "text", "text|json")
	overridesFile := fs.String("overrides", "wirefit-overrides.yaml", "rule overrides file (optional)")
	reportFile := fs.String("report", "", "also write a markdown report to this path (for PR/MR comments)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *repoDir == "" {
		fmt.Fprintln(os.Stderr, "wirefit check: --contracts-repo is required")
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}
	ovr, ovrErrs := override.Load(*overridesFile, time.Now())
	if len(ovrErrs) > 0 {
		for _, e := range ovrErrs {
			fmt.Fprintln(os.Stderr, "wirefit check:", e)
		}
		return 1 // invalid/expired overrides gate the merge (PRD 3.3)
	}
	pol, err := policy.Load(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit check:", err)
		return 2
	}
	for _, o := range ovr.Overrides {
		if !pol.Overridable(o.Rule) {
			fmt.Fprintf(os.Stderr, "wirefit check: org policy forbids overriding rule %s (policy.yaml in the contracts repo)\n", o.Rule)
			return 1
		}
	}
	st, err := store.Open(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit check:", err)
		return 2
	}

	worst := 0
	results := map[string]*diff.Result{}

	// Provider side: my published schema vs my candidate, against registered consumers.
	for _, p := range m.Provides {
		dir, err := diff.ParseDirection(p.Direction)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit check:", err)
			return 2
		}
		candidate, err := ir.Load(filepath.Join(*irDir, "provides", p.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit check: missing candidate IR for %s; run `wirefit extract` first (%v)\n", p.ID, err)
			return 2
		}
		published, ok, err := st.ProviderIR(m.Service, p.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit check:", err)
			return 2
		}
		consumers, err := st.ConsumersOf(m.Service, p.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit check:", err)
			return 2
		}
		if !ok {
			results["provides "+p.ID] = &diff.Result{Direction: dir, Findings: []diff.Finding{{
				Class: diff.Neutral, Rule: "new-interaction", Path: "$",
				Message: "not yet published; first publish will register it",
			}}}
			continue
		}
		r := diff.Self(published, candidate, diff.SelfOptions{
			Direction:              dir,
			Consumers:              consumers,
			ProviderRejectsUnknown: m.RejectsUnknown(),
			ColdStart:              len(consumers) == 0,
		})
		pol.Apply(r)
		ovr.Apply(p.ID, r)
		results["provides "+p.ID] = r
		if r.ExitCode() > worst {
			worst = r.ExitCode()
		}
	}

	// Consumer side: my candidate expectations vs each provider's published schema.
	for _, c := range m.Consumes {
		mine, err := ir.Load(filepath.Join(*irDir, "consumes", c.Provider, c.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit check: missing candidate IR for consumed %s/%s; run `wirefit extract` first (%v)\n", c.Provider, c.ID, err)
			return 2
		}
		key := "consumes " + c.Provider + "/" + c.ID
		provIR, ok, err := st.ProviderIR(c.Provider, c.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit check:", err)
			return 2
		}
		if !ok {
			results[key] = &diff.Result{Findings: []diff.Finding{{
				Class: diff.Warning, Rule: "provider-unpublished", Path: "$",
				Message: "provider has not published this interaction; nothing to check against",
			}}}
			continue
		}
		provManifest, err := st.ServiceManifest(c.Provider)
		if err != nil || provManifest == nil {
			results[key] = &diff.Result{Findings: []diff.Finding{{
				Class: diff.Warning, Rule: "provider-manifest-missing", Path: "$",
				Message: "provider manifest copy missing in contracts repo",
			}}}
			continue
		}
		var dir diff.Direction
		found := false
		for _, p := range provManifest.Provides {
			if p.ID == c.ID {
				dir, err = diff.ParseDirection(p.Direction)
				if err == nil {
					found = true
				}
			}
		}
		if !found {
			results[key] = &diff.Result{Findings: []diff.Finding{{
				Class: diff.Warning, Rule: "interaction-unknown-to-provider", Path: "$",
				Message: "provider's manifest does not declare this interaction id",
			}}}
			continue
		}
		strict := false
		if dir == diff.P2C {
			strict = m.RejectsUnknown() // I parse the response
		} else {
			strict = provManifest.RejectsUnknown() // provider parses my request
		}
		r := diff.Compat(provIR, mine, diff.CompatOptions{Direction: dir, StrictParser: strict})
		pol.Apply(r)
		ovr.Apply(c.ID, r)
		results[key] = r
		if r.ExitCode() > worst {
			worst = r.ExitCode()
		}
	}

	// Stale overrides (matched nothing) fail loudly: the path or rule they
	// referenced no longer exists, so they must be removed (PRD 3.3).
	if stale := ovr.Stale(); len(stale) > 0 {
		sr := &diff.Result{Findings: []diff.Finding{}}
		for _, o := range stale {
			sr.Findings = append(sr.Findings, diff.Finding{
				Class: diff.Breaking, Rule: "override-stale", Path: o.Path,
				Message: fmt.Sprintf("override on %s/%s matched no finding; remove it (%s)",
					o.Interaction, o.Rule, o.Justification),
			})
		}
		results["overrides"] = sr
		worst = 1
	}

	// Persist machine-readable results for `wirefit override add` auto-fill.
	if lcJSON, err := json.Marshal(results); err == nil {
		if err := os.MkdirAll(filepath.Dir(lastCheckFile), 0o755); err == nil {
			_ = os.WriteFile(lastCheckFile, lcJSON, 0o644)
		}
	}
	if *reportFile != "" {
		if err := os.MkdirAll(filepath.Dir(*reportFile), 0o755); err == nil {
			_ = os.WriteFile(*reportFile, renderMarkdown(results, worst), 0o644)
		}
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
		return worst
	}
	printCheck(m.Service, results, worst)
	return worst
}

func sortedResultKeys(m map[string]*diff.Result) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // insertion sort: tiny n, zero imports
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// ---------------------------------------------------------------- publish --

func cmdPublish(args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	mf := fs.String("f", "contracts.yaml", "manifest file")
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	irDir := fs.String("ir", ".wirefit/ir", "directory with extracted IR")
	noCommit := fs.Bool("no-commit", false, "write files without git commit/push")
	if fs.Parse(args) != nil {
		return 2
	}
	if *repoDir == "" {
		fmt.Fprintln(os.Stderr, "wirefit publish: --contracts-repo is required")
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}
	st, err := store.Open(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit publish:", err)
		return 2
	}

	provides := map[string][]byte{}
	for _, p := range m.Provides {
		raw, err := os.ReadFile(filepath.Join(*irDir, "provides", p.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit publish: missing IR for %s; run `wirefit extract` first\n", p.ID)
			return 2
		}
		provides[p.ID] = raw
	}
	consumes := map[string]map[string][]byte{}
	for _, c := range m.Consumes {
		raw, err := os.ReadFile(filepath.Join(*irDir, "consumes", c.Provider, c.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit publish: missing IR for consumed %s/%s; run `wirefit extract` first\n", c.Provider, c.ID)
			return 2
		}
		if consumes[c.Provider] == nil {
			consumes[c.Provider] = map[string][]byte{}
		}
		consumes[c.Provider][c.ID] = raw
	}
	if err := st.Publish(m, *mf, provides, consumes, *noCommit); err != nil {
		fmt.Fprintln(os.Stderr, "wirefit publish:", err)
		return 2
	}
	fmt.Printf("published %s: %d provided, %d consumed\n",
		m.Service, len(provides), len(m.Consumes))
	return 0
}
