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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/gotool"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/javatool"
	"github.com/wirefit/wirefit/internal/manifest"
	"github.com/wirefit/wirefit/internal/override"
	"github.com/wirefit/wirefit/internal/policy"
	"github.com/wirefit/wirefit/internal/store"
	"github.com/wirefit/wirefit/internal/tstool"
)

// isTSSpec reports whether a manifest dto reference targets the TypeScript
// extractor: a file path ending in .ts/.tsx with a "#TypeName" selector.
func isTSSpec(dto string) bool {
	file, _, _ := strings.Cut(dto, "#")
	return strings.HasSuffix(file, ".ts") || strings.HasSuffix(file, ".tsx")
}

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
	classpath := fs.String("classpath", "", "java: service classpath override (skips build-tool resolution)")
	buildTool := fs.String("build-tool", "auto", "java: auto|maven|gradle|none — how to resolve the service classpath")
	projectDir := fs.String("project", ".", "service project directory")
	extractorCP := fs.String("extractor-cp", os.Getenv("WIREFIT_EXTRACTOR_CP"),
		"override for WirefitExtract+jackson classpath (default: self-bootstrapped cache)")
	mapperHint := fs.String("mapper", "", "ObjectMapper provider <class-fqn>#<static-method> (overrides manifest settings.java-mapper)")
	javaBin := fs.String("java", "java", "java binary")
	irDir := fs.String("ir", ".wirefit/ir", "output directory for extracted IR")
	if fs.Parse(args) != nil {
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}

	// dto → list of relative output paths (the same DTO may back several interactions).
	targets := map[string][]string{}
	for _, p := range m.Provides {
		targets[p.DTO] = append(targets[p.DTO], filepath.Join("provides", p.ID+".ir.json"))
	}
	for _, c := range m.Consumes {
		targets[c.DTO] = append(targets[c.DTO], filepath.Join("consumes", c.Provider, c.ID+".ir.json"))
	}

	// Language routing per DTO reference (PRD 2.1): "path/file.ts#Type" → ts,
	// java FQN → java. Mixed manifests extract with both toolchains. The TS
	// side also needs the manifest role per spec: Zod `.default()` fields are
	// required on the provider (output) side but optional on the consumer
	// (input) side (PRD 2.3).
	provided := map[string]bool{}
	for _, p := range m.Provides {
		provided[p.DTO] = true
	}
	consumed := map[string]bool{}
	for _, c := range m.Consumes {
		consumed[c.DTO] = true
	}
	external := func(dto string) []string { // third-party extractor command (PRD 3.2)
		file, _, _ := strings.Cut(dto, "#")
		for _, x := range m.Extractors {
			if strings.HasSuffix(file, x.Match) {
				return strings.Fields(x.Command)
			}
		}
		return nil
	}
	var javaFQNs, goSpecs, tsProvided, tsConsumed []string
	extReqs := map[string]*extproto.Request{} // command line → request
	for dto := range targets {
		switch {
		case external(dto) != nil:
			cmdLine := strings.Join(external(dto), "\x00")
			req := extReqs[cmdLine]
			if req == nil {
				req = &extproto.Request{ProjectDir: *projectDir}
				extReqs[cmdLine] = req
			}
			role := "consumed"
			if provided[dto] {
				role = "provided"
			}
			req.Specs = append(req.Specs, extproto.Spec{Ref: dto, Role: role})
		case gotool.IsSpec(dto):
			goSpecs = append(goSpecs, dto)
		case !isTSSpec(dto):
			javaFQNs = append(javaFQNs, dto)
		case provided[dto] && consumed[dto]:
			fmt.Fprintf(os.Stderr, "wirefit extract: %s is used in both provides and consumes — split the schema (zod io semantics differ per side)\n", dto)
			return 2
		case provided[dto]:
			tsProvided = append(tsProvided, dto)
		default:
			tsConsumed = append(tsConsumed, dto)
		}
	}
	sort.Strings(javaFQNs)
	sort.Strings(goSpecs)
	sort.Strings(tsProvided)
	sort.Strings(tsConsumed)

	extracted := map[string]json.RawMessage{}

	for cmdLine, req := range extReqs {
		sort.Slice(req.Specs, func(i, j int) bool { return req.Specs[i].Ref < req.Specs[j].Ref })
		resp, err := extproto.Invoke(strings.Split(cmdLine, "\x00"), *req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract:", err)
			return 2
		}
		for k, v := range resp.Schemas {
			extracted[k] = v
		}
	}

	if len(goSpecs) > 0 {
		goOut, err := gotool.Run(*projectDir, goSpecs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract:", err)
			return 2
		}
		for k, v := range goOut {
			extracted[k] = v
		}
	}

	if len(tsProvided)+len(tsConsumed) > 0 {
		tsOut, err := tstool.Run(*projectDir, tsProvided, tsConsumed)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract:", err)
			return 2
		}
		for k, v := range tsOut {
			extracted[k] = v
		}
	}

	if len(javaFQNs) > 0 {
		// Service classpath: explicit override, or interrogate the build tool —
		// zero build-file changes required in the service (PRD 1.3 amendment).
		serviceCP := *classpath
		if serviceCP == "" {
			var err error
			serviceCP, err = javatool.ResolveClasspath(*projectDir, *buildTool)
			if err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
		}
		// Extractor classpath: self-bootstrapping cache (pinned, checksummed
		// jars + embedded WirefitExtract compiled on demand) unless overridden.
		if *extractorCP == "" {
			var err error
			*extractorCP, err = javatool.EnsureExtractor()
			if err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
		}
		mapper := *mapperHint
		if mapper == "" {
			mapper = m.Settings.JavaMapper
		}
		// Service classpath first: the service's own Jackson version wins.
		javaArgs := []string{"-cp", serviceCP + string(os.PathListSeparator) + *extractorCP}
		if mapper != "" {
			javaArgs = append(javaArgs, "-Dwirefit.mapper="+mapper)
		}
		javaArgs = append(javaArgs, "io.wirefit.extract.WirefitExtract")
		javaArgs = append(javaArgs, javaFQNs...)
		cmd := exec.Command(*javaBin, javaArgs...)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract: extractor failed:", err)
			return 2
		}
		var javaOut map[string]json.RawMessage
		if err := json.Unmarshal(out, &javaOut); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit extract: bad extractor output:", err)
			return 2
		}
		for k, v := range javaOut {
			extracted[k] = v
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
		for _, rel := range rels {
			p := filepath.Join(*irDir, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
			if err := os.WriteFile(p, append(canon, '\n'), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "wirefit extract:", err)
				return 2
			}
		}
	}
	fmt.Printf("extracted %d schema(s) into %s\n", len(targets), *irDir)
	return 0
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
			fmt.Fprintf(os.Stderr, "wirefit check: missing candidate IR for %s — run `wirefit extract` first (%v)\n", p.ID, err)
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
				Message: "not yet published — first publish will register it",
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
			fmt.Fprintf(os.Stderr, "wirefit check: missing candidate IR for consumed %s/%s — run `wirefit extract` first (%v)\n", c.Provider, c.ID, err)
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
				Message: "provider has not published this interaction — nothing to check against",
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
				Message: fmt.Sprintf("override on %s/%s matched no finding — remove it (%s)",
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
	for _, key := range sortedResultKeys(results) {
		fmt.Printf("— %s\n", key)
		printResult(results[key], "text")
		fmt.Println()
	}
	if worst == 0 {
		fmt.Println("wirefit check: all interactions compatible")
	} else {
		fmt.Println("wirefit check: BREAKING changes found")
	}
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
			fmt.Fprintf(os.Stderr, "wirefit publish: missing IR for %s — run `wirefit extract` first\n", p.ID)
			return 2
		}
		provides[p.ID] = raw
	}
	consumes := map[string]map[string][]byte{}
	for _, c := range m.Consumes {
		raw, err := os.ReadFile(filepath.Join(*irDir, "consumes", c.Provider, c.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit publish: missing IR for consumed %s/%s — run `wirefit extract` first\n", c.Provider, c.ID)
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
	fmt.Printf("published %s: %d provided, %d consumed interaction(s)\n",
		m.Service, len(provides), len(m.Consumes))
	return 0
}
