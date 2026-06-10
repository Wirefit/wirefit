package main

// Phase 4: environment awareness. `record-deploy` pins what runs where;
// `can-i-deploy` answers the question HEAD-vs-HEAD checks cannot: is this
// candidate compatible with what is ACTUALLY DEPLOYED right now;
// `matrix` renders the whole org's deployed compatibility state.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/store"
)

func recordedBy() string {
	for _, v := range []string{"WIREFIT_DEPLOYER", "GITHUB_ACTOR", "GITLAB_USER_LOGIN", "USER"} {
		if s := os.Getenv(v); s != "" {
			return s
		}
	}
	return "unknown"
}

// ------------------------------------------------------------ record-deploy --

func cmdRecordDeploy(args []string) int {
	fs := flag.NewFlagSet("record-deploy", flag.ContinueOnError)
	mf := fs.String("f", "contracts.yaml", "manifest file")
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	env := fs.String("env", "", "environment name (e.g. production)")
	noCommit := fs.Bool("no-commit", false, "write files without git commit/push")
	if fs.Parse(args) != nil {
		return 2
	}
	if *repoDir == "" || *env == "" {
		fmt.Fprintln(os.Stderr, "wirefit record-deploy: --contracts-repo and --env are required")
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}
	st, err := store.Open(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit record-deploy:", err)
		return 2
	}

	entry := &store.ServiceLock{
		RecordedAt: time.Now().UTC().Truncate(time.Second),
		RecordedBy: recordedBy(),
		Provides:   map[string]string{},
		Consumes:   map[string]string{},
	}
	record := func(rel string, into map[string]string, key string) int {
		p := filepath.Join(*repoDir, "contracts", m.Service, rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit record-deploy: %s not published — run `wirefit publish` before recording deploys (%v)\n", key, err)
			return 2
		}
		hash, err := st.WriteBlob(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit record-deploy:", err)
			return 2
		}
		into[key] = hash
		return 0
	}
	for _, p := range m.Provides {
		if c := record(filepath.Join("provides", p.ID+".ir.json"), entry.Provides, p.ID); c != 0 {
			return c
		}
	}
	for _, c := range m.Consumes {
		if code := record(filepath.Join("consumes", c.Provider, c.ID+".ir.json"),
			entry.Consumes, c.Provider+"/"+c.ID); code != 0 {
			return code
		}
	}

	lock, err := st.LoadEnvLock(*env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit record-deploy:", err)
		return 2
	}
	lock[m.Service] = entry
	if err := st.SaveEnvLock(*env, lock); err != nil {
		fmt.Fprintln(os.Stderr, "wirefit record-deploy:", err)
		return 2
	}
	if !*noCommit {
		if err := st.CommitPaths(fmt.Sprintf("wirefit record-deploy: %s → %s", m.Service, *env), "_envs", "_blobs"); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit record-deploy:", err)
			return 2
		}
	}
	fmt.Printf("recorded %s in %s: %d provided, %d consumed interaction(s) (by %s)\n",
		m.Service, *env, len(entry.Provides), len(entry.Consumes), entry.RecordedBy)
	return 0
}

// ------------------------------------------------------------- can-i-deploy --

func cmdCanIDeploy(args []string) int {
	fs := flag.NewFlagSet("can-i-deploy", flag.ContinueOnError)
	mf := fs.String("f", "contracts.yaml", "manifest file")
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	env := fs.String("env", "", "target environment")
	irDir := fs.String("ir", ".wirefit/ir", "candidate IR (from `wirefit extract` at the release commit)")
	staleDays := fs.Int("stale-days", 30, "deploy records older than this are labeled stale")
	format := fs.String("format", "text", "text|json")
	reportFile := fs.String("report", "", "also write a markdown report")
	if fs.Parse(args) != nil {
		return 2
	}
	if *repoDir == "" || *env == "" {
		fmt.Fprintln(os.Stderr, "wirefit can-i-deploy: --contracts-repo and --env are required")
		return 2
	}
	m, code := loadManifest(*mf)
	if code != 0 {
		return code
	}
	st, err := store.Open(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
		return 2
	}
	lock, err := st.LoadEnvLock(*env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
		return 2
	}

	staleBefore := time.Now().Add(-time.Duration(*staleDays) * 24 * time.Hour)
	strictOf := func(svc string) bool {
		if sm, err := st.ServiceManifest(svc); err == nil && sm != nil {
			return sm.RejectsUnknown()
		}
		return false
	}
	dirOf := func(provider, id string) (diff.Direction, bool) {
		pm, err := st.ServiceManifest(provider)
		if err != nil || pm == nil {
			return "", false
		}
		for _, p := range pm.Provides {
			if p.ID == id {
				d, err := diff.ParseDirection(p.Direction)
				return d, err == nil
			}
		}
		return "", false
	}

	worst := 0
	results := map[string]*diff.Result{}
	bump := func(key string, r *diff.Result) {
		results[key] = r
		if r.ExitCode() > worst {
			worst = r.ExitCode()
		}
	}

	// Provider side: does my candidate still satisfy every DEPLOYED consumer?
	for _, p := range m.Provides {
		dir, err := diff.ParseDirection(p.Direction)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
			return 2
		}
		candidate, err := ir.Load(filepath.Join(*irDir, "provides", p.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit can-i-deploy: missing candidate IR for %s — run `wirefit extract` first (%v)\n", p.ID, err)
			return 2
		}
		// Consumers registered at main, for untracked detection (PRD 4.4).
		mainConsumers, err := st.ConsumersOf(m.Service, p.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
			return 2
		}
		for svc, sl := range lock {
			if svc == m.Service {
				continue
			}
			hash, ok := sl.Consumes[m.Service+"/"+p.ID]
			if !ok {
				continue
			}
			proj, err := st.ReadBlob(hash)
			if err != nil {
				fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
				return 2
			}
			r := diff.Compat(candidate, proj, diff.CompatOptions{Direction: dir, StrictParser: strictOf(svc)})
			if sl.RecordedAt.Before(staleBefore) {
				r.Findings = append(r.Findings, diff.Finding{
					Class: diff.Warning, Rule: "stale-deploy-record", Path: "$",
					Message: fmt.Sprintf("deploy record from %s is older than %d days — re-record to trust this result",
						sl.RecordedAt.Format("2006-01-02"), *staleDays),
				})
			}
			delete(mainConsumers, svc)
			bump(fmt.Sprintf("provides %s ⇐ %s@%s", p.ID, svc, *env), r)
		}
		// Registered at main but never deploy-recorded: never silently green.
		for svc, c := range mainConsumers {
			r := diff.Compat(candidate, c.Schema, diff.CompatOptions{Direction: dir, StrictParser: c.RejectUnknown})
			r.Findings = append(r.Findings, diff.Finding{
				Class: diff.Warning, Rule: "untracked-consumer", Path: "$",
				Message: fmt.Sprintf("%s has no deploy record in %s — checked against its main-branch usage instead", svc, *env),
			})
			bump(fmt.Sprintf("provides %s ⇐ %s (untracked)", p.ID, svc), r)
		}
	}

	// Consumer side: does the DEPLOYED provider satisfy my candidate expectations?
	for _, c := range m.Consumes {
		mine, err := ir.Load(filepath.Join(*irDir, "consumes", c.Provider, c.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit can-i-deploy: missing candidate IR for consumed %s/%s (%v)\n", c.Provider, c.ID, err)
			return 2
		}
		dir, ok := dirOf(c.Provider, c.ID)
		if !ok {
			bump("consumes "+c.Provider+"/"+c.ID, &diff.Result{Findings: []diff.Finding{{
				Class: diff.Warning, Rule: "provider-unpublished", Path: "$",
				Message: "provider has not published this interaction",
			}}})
			continue
		}
		strict := m.RejectsUnknown()
		if dir == diff.C2P {
			strict = strictOf(c.Provider)
		}
		var provIR *ir.Schema
		label := fmt.Sprintf("consumes %s/%s @%s", c.Provider, c.ID, *env)
		var extra *diff.Finding
		if sl, ok := lock[c.Provider]; ok {
			if hash, ok := sl.Provides[c.ID]; ok {
				provIR, err = st.ReadBlob(hash)
				if err != nil {
					fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
					return 2
				}
				if sl.RecordedAt.Before(staleBefore) {
					extra = &diff.Finding{Class: diff.Warning, Rule: "stale-deploy-record", Path: "$",
						Message: fmt.Sprintf("provider deploy record from %s is older than %d days", sl.RecordedAt.Format("2006-01-02"), *staleDays)}
				}
			}
		}
		if provIR == nil {
			provIR, ok, err = st.ProviderIR(c.Provider, c.ID)
			if err != nil || !ok {
				bump(label, &diff.Result{Findings: []diff.Finding{{
					Class: diff.Warning, Rule: "untracked-provider", Path: "$",
					Message: "provider has neither a deploy record nor a published schema",
				}}})
				continue
			}
			label = fmt.Sprintf("consumes %s/%s (untracked)", c.Provider, c.ID)
			extra = &diff.Finding{Class: diff.Warning, Rule: "untracked-provider", Path: "$",
				Message: fmt.Sprintf("%s has no deploy record in %s — checked against its main-branch schema instead", c.Provider, *env)}
		}
		r := diff.Compat(provIR, mine, diff.CompatOptions{Direction: dir, StrictParser: strict})
		if extra != nil {
			r.Findings = append(r.Findings, *extra)
		}
		bump(label, r)
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
		fmt.Printf("wirefit can-i-deploy: SAFE to deploy to %s\n", *env)
	} else {
		fmt.Printf("wirefit can-i-deploy: DO NOT deploy to %s\n", *env)
	}
	return worst
}

// -------------------------------------------------------------------- matrix --

func cmdMatrix(args []string) int {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	staleDays := fs.Int("stale-days", 30, "deploy records older than this are labeled stale")
	format := fs.String("format", "md", "md|json")
	if fs.Parse(args) != nil {
		return 2
	}
	if *repoDir == "" {
		fmt.Fprintln(os.Stderr, "wirefit matrix: --contracts-repo is required")
		return 2
	}
	st, err := store.Open(*repoDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
		return 2
	}
	staleBefore := time.Now().Add(-time.Duration(*staleDays) * 24 * time.Hour)

	type edge struct {
		Env, Consumer, Provider, Interaction, Status, Detail string
	}
	var edges []edge
	for _, env := range st.Envs() {
		lock, err := st.LoadEnvLock(env)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
			return 2
		}
		for consumer, sl := range lock {
			for key, chash := range sl.Consumes {
				provider, id, _ := cutString(key, "/")
				e := edge{Env: env, Consumer: consumer, Provider: provider, Interaction: id}
				psl, ok := lock[provider]
				var phash string
				if ok {
					phash, ok = psl.Provides[id]
				}
				if !ok {
					e.Status, e.Detail = "untracked", "provider has no deploy record in this env"
					edges = append(edges, e)
					continue
				}
				proj, err1 := st.ReadBlob(chash)
				prov, err2 := st.ReadBlob(phash)
				if err1 != nil || err2 != nil {
					e.Status, e.Detail = "error", "missing blob — re-publish + re-record"
					edges = append(edges, e)
					continue
				}
				dir := diff.P2C
				if pm, err := st.ServiceManifest(provider); err == nil && pm != nil {
					for _, p := range pm.Provides {
						if p.ID == id {
							if d, err := diff.ParseDirection(p.Direction); err == nil {
								dir = d
							}
						}
					}
				}
				strict := false
				if cm, err := st.ServiceManifest(consumer); err == nil && cm != nil {
					strict = cm.RejectsUnknown()
				}
				r := diff.Compat(prov, proj, diff.CompatOptions{Direction: dir, StrictParser: strict})
				switch r.Max() {
				case diff.Breaking:
					e.Status, e.Detail = "INCOMPATIBLE", r.Findings[0].Message
				case diff.Warning:
					e.Status, e.Detail = "warning", r.Findings[0].Message
				default:
					e.Status = "ok"
				}
				if sl.RecordedAt.Before(staleBefore) || psl.RecordedAt.Before(staleBefore) {
					e.Detail = trimJoin(e.Detail, "stale deploy record")
				}
				edges = append(edges, e)
			}
		}
	}

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(edges)
	} else {
		fmt.Println("# wirefit deployed compatibility matrix")
		fmt.Println()
		fmt.Println("| env | consumer | provider / interaction | status | detail |")
		fmt.Println("|---|---|---|---|---|")
		for _, e := range edges {
			icon := map[string]string{"ok": "✅", "warning": "⚠️", "INCOMPATIBLE": "🔴", "untracked": "⚪", "error": "❗"}[e.Status]
			fmt.Printf("| %s | %s | %s / %s | %s %s | %s |\n", e.Env, e.Consumer, e.Provider, e.Interaction, icon, e.Status, e.Detail)
		}
	}
	for _, e := range edges {
		if e.Status == "INCOMPATIBLE" || e.Status == "error" {
			return 1
		}
	}
	return 0
}

func cutString(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func trimJoin(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
