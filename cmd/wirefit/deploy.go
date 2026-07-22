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
	"sort"
	"strings"
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
			fmt.Fprintf(os.Stderr, "wirefit record-deploy: %s not published; run `wirefit publish` before recording deploys (%v)\n", key, err)
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
	fromEnv := fs.String("from-env", "", "promotion gate: candidate = what is deploy-recorded in this env (--ir is ignored)")
	service := fs.String("service", "", "with --from-env: the service to promote, so no manifest checkout is needed")
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
	var cand candidate
	if *fromEnv == "" {
		m, code := loadManifest(*mf)
		if code != 0 {
			return code
		}
		if cand, code = candidateFromIR(m, *irDir); code != 0 {
			return code
		}
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
	if *fromEnv != "" {
		// Promotion gate: the candidate is what already runs in --from-env.
		// Directions and strictness then come from the published manifest,
		// not a local checkout (a deploy pipeline has no service repo).
		svc := *service
		if svc == "" {
			m, code := loadManifest(*mf)
			if code != 0 {
				return code
			}
			svc = m.Service
		}
		fromLock, err := st.LoadEnvLock(*fromEnv)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
			return 2
		}
		sl, ok := fromLock[svc]
		if !ok {
			fmt.Fprintf(os.Stderr, "wirefit can-i-deploy: %s has no deploy record in %s; run `wirefit record-deploy` there first\n", svc, *fromEnv)
			return 2
		}
		if cand, err = candidateFromLock(st, svc, sl); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
			return 2
		}
	}

	staleBefore := time.Now().Add(-time.Duration(*staleDays) * 24 * time.Hour)
	drs, err := evalDeploy(st, cand, *env, lock, staleBefore, *staleDays)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
		return 2
	}
	worst := 0
	results := map[string]*diff.Result{}
	for _, dr := range drs {
		if dr.err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", dr.err)
			return 2
		}
		results[canIDeployLabel(dr, *env)] = dr.res
		if dr.res.ExitCode() > worst {
			worst = dr.res.ExitCode()
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
		fmt.Printf("· %s\n", key)
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

// matrixEdge is one consumer→provider dependency in one env; the field names
// are the `--format json` contract.
type matrixEdge struct {
	Env, Consumer, Provider, Interaction string
	Status                               matrixStatus
	Detail                               string
	// Findings and the deploy records feed the HTML detail view; Detail stays
	// the one-line summary so term/md output is unchanged.
	Findings                       []diff.Finding `json:",omitempty"`
	ConsumerRecord, ProviderRecord *deployRecord  `json:",omitempty"`
	// ConsumerBody/ProviderBody feed the HTML modal's side-by-side view; kept
	// out of the --format json contract (the blobs are available via the store).
	ConsumerBody, ProviderBody *ir.Schema `json:"-"`
}

// deployRecord is one side's deploy-record provenance, surfaced by the HTML
// detail view; additive `--format json` fields.
type deployRecord struct {
	RecordedAt string // RFC3339 UTC, straight from the stored lock (stable, NF3)
	RecordedBy string
	Hash       string // first 12 hex of the sha256 blob hash
	Version    int    `json:",omitempty"` // publish counter; 0 when unknown (published before version logs)
}

// Label is the display form of the deployed version: the publish counter when
// known, the content hash for records from before version logs existed.
func (r *deployRecord) Label() string {
	if r.Version > 0 {
		return fmt.Sprintf("v%d", r.Version)
	}
	return r.Hash
}

func shortHash(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		h = h[:12]
	}
	return h
}

func newDeployRecord(sl *store.ServiceLock, hash string, ver int) *deployRecord {
	return &deployRecord{
		RecordedAt: sl.RecordedAt.UTC().Format(time.RFC3339),
		RecordedBy: sl.RecordedBy,
		Hash:       shortHash(hash),
		Version:    ver,
	}
}

type deployRecordResolver struct {
	st   *store.Store
	logs map[string]store.VersionLog
}

func newDeployRecordResolver(st *store.Store) *deployRecordResolver {
	return &deployRecordResolver{st: st, logs: map[string]store.VersionLog{}}
}

func (r *deployRecordResolver) resolve(service string, sl *store.ServiceLock, ref, hash string) (*deployRecord, error) {
	if sl == nil || hash == "" {
		return nil, nil
	}
	v, ok := r.logs[service]
	if !ok {
		var err error
		if v, err = r.st.LoadVersions(service); err != nil {
			return nil, err
		}
		r.logs[service] = v
	}
	return newDeployRecord(sl, hash, v.Resolve(ref, hash)), nil
}

type matrixStatus string

const (
	matrixStatusOK           matrixStatus = "ok"
	matrixStatusWarning      matrixStatus = "warning"
	matrixStatusIncompatible matrixStatus = "INCOMPATIBLE"
	matrixStatusUntracked    matrixStatus = "untracked"
	matrixStatusError        matrixStatus = "error"
)

// matrixDoc is the `matrix --format json` document: the within-env edges
// plus the promotion checks (empty without a pipeline).
type matrixDoc struct {
	Deployed   []matrixEdge `json:"deployed"`
	Promotions []promoEdge  `json:"promotions"`
}

func newMatrixDoc(edges []matrixEdge, promos []promoEdge) matrixDoc {
	// nil slices marshal as null; the contract is always two arrays.
	if edges == nil {
		edges = []matrixEdge{}
	}
	if promos == nil {
		promos = []promoEdge{}
	}
	return matrixDoc{Deployed: edges, Promotions: promos}
}

func cmdMatrix(args []string) int {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	repoDir := fs.String("contracts-repo", "", "path to a contracts repo working copy")
	staleDays := fs.Int("stale-days", 30, "deploy records older than this are labeled stale")
	envsFlag := fs.String("envs", "", "comma-separated promotion order (overrides _envs/pipeline.yaml)")
	format := fs.String("format", "term", "term|md|html|json")
	out := fs.String("o", "", "also write the matrix to this file (.md, .html or .json)")
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
	edges, err := matrixEdges(st, staleBefore)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
		return 2
	}
	pipeline, err := st.LoadPipeline()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
		return 2
	}
	if *envsFlag != "" {
		pipeline = strings.Split(*envsFlag, ",")
		if err := store.ValidatePipelineEnvs(pipeline); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit matrix: --envs:", err)
			return 2
		}
	}
	var promos []promoEdge
	if len(pipeline) > 0 {
		if promos, err = promoEdges(st, pipeline, staleBefore, *staleDays); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
			return 2
		}
	}
	inv, err := serviceInventory(st, staleBefore)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
		return 2
	}

	switch *format {
	case "term":
		printMatrixTerm(edges)
		printPromoTerm(promos)
	case "md":
		os.Stdout.Write(renderMatrixMD(edges, promos))
	case "html":
		os.Stdout.Write(renderMatrixHTML(edges, promos, pipeline, inv))
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(newMatrixDoc(edges, promos))
	default:
		fmt.Fprintf(os.Stderr, "wirefit matrix: unknown format %q (term|md|html|json)\n", *format)
		return 2
	}
	if *out != "" {
		var data []byte
		switch filepath.Ext(*out) {
		case ".md":
			data = renderMatrixMD(edges, promos)
		case ".html", ".htm":
			data = renderMatrixHTML(edges, promos, pipeline, inv)
		case ".json":
			data, _ = json.MarshalIndent(newMatrixDoc(edges, promos), "", "  ")
			data = append(data, '\n')
		default:
			fmt.Fprintf(os.Stderr, "wirefit matrix: cannot infer format for %s; use a .md, .html or .json extension\n", *out)
			return 2
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit matrix:", err)
			return 2
		}
	}
	for _, e := range edges {
		if e.Status == matrixStatusIncompatible || e.Status == matrixStatusError {
			return 1
		}
	}
	return 0
}

func matrixEdges(st *store.Store, staleBefore time.Time) ([]matrixEdge, error) {
	records := newDeployRecordResolver(st)
	var edges []matrixEdge
	for _, env := range st.Envs() {
		lock, err := st.LoadEnvLock(env)
		if err != nil {
			return nil, err
		}
		for consumer, sl := range lock {
			for key, chash := range sl.Consumes {
				provider, id, _ := cutString(key, "/")
				consumerRecord, err := records.resolve(consumer, sl, "consumes/"+key, chash)
				if err != nil {
					return nil, err
				}
				e := matrixEdge{Env: env, Consumer: consumer, Provider: provider, Interaction: id,
					ConsumerRecord: consumerRecord}
				psl, ok := lock[provider]
				var phash string
				if ok {
					phash, ok = psl.Provides[id]
				}
				if !ok {
					e.Status, e.Detail = matrixStatusUntracked, "provider has no deploy record in this env"
					edges = append(edges, e)
					continue
				}
				proj, cerr := st.ReadBlob(chash)
				prov, perr := st.ReadBlob(phash)
				if cerr != nil || perr != nil {
					e.Status, e.Detail = matrixStatusError, "missing blob; re-publish + re-record"
					edges = append(edges, e)
					continue
				}
				e.ConsumerBody, e.ProviderBody = proj, prov
				e.ProviderRecord, err = records.resolve(provider, psl, "provides/"+id, phash)
				if err != nil {
					return nil, err
				}
				dir := diff.P2C
				if d, ok := dirOf(st, provider, id); ok {
					dir = d
				}
				r := diff.Compat(prov, proj, diff.CompatOptions{Direction: dir, StrictParser: strictOf(st, consumer)})
				e.Findings = r.Findings
				switch r.Max() {
				case diff.Breaking:
					e.Status, e.Detail = matrixStatusIncompatible, r.Findings[0].Message
				case diff.Warning:
					e.Status, e.Detail = matrixStatusWarning, r.Findings[0].Message
				default:
					e.Status = matrixStatusOK
				}
				if sl.RecordedAt.Before(staleBefore) || psl.RecordedAt.Before(staleBefore) {
					e.Detail = trimJoin(e.Detail, "stale deploy record")
				}
				edges = append(edges, e)
			}
		}
	}
	// env locks are maps; sort so identical inputs render identically (NF3).
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Env != b.Env {
			return a.Env < b.Env
		}
		if a.Consumer != b.Consumer {
			return a.Consumer < b.Consumer
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Interaction < b.Interaction
	})
	return edges, nil
}

// invItem is one pinned interaction in a service's deploy record; Key is the
// interaction id (provides) or "provider/interaction" (consumes).
type invItem struct {
	Key    string
	Record *deployRecord
}

// invEnv is one service's deploy record in one env, hashes resolved to
// publish-counter versions. RecordedAt is RFC3339 UTC like deployRecord.
type invEnv struct {
	Env                    string
	RecordedAt, RecordedBy string
	Stale                  bool
	Provides, Consumes     []invItem // Key-sorted
}

// invService is one deploy-recorded service across every env: the service
// directory's ground truth, independent of who consumes what. Feeds only the
// HTML report; the --format json document is unchanged.
type invService struct {
	Service string
	Envs    []invEnv // env-name order (st.Envs is sorted)
}

// serviceInventory walks every env lock so the report can list services and
// provided interactions that no consumer edge reaches (a provider nobody
// consumes would otherwise vanish).
func serviceInventory(st *store.Store, staleBefore time.Time) ([]invService, error) {
	records := newDeployRecordResolver(st)
	byName := map[string]*invService{}
	for _, env := range st.Envs() {
		lock, err := st.LoadEnvLock(env)
		if err != nil {
			return nil, err
		}
		for _, svc := range sortedLockKeys(lock) {
			sl := lock[svc]
			ie := invEnv{Env: env,
				RecordedAt: sl.RecordedAt.UTC().Format(time.RFC3339), RecordedBy: sl.RecordedBy,
				Stale: sl.RecordedAt.Before(staleBefore)}
			for _, id := range sortedKeys(sl.Provides) {
				rec, err := records.resolve(svc, sl, "provides/"+id, sl.Provides[id])
				if err != nil {
					return nil, err
				}
				ie.Provides = append(ie.Provides, invItem{Key: id, Record: rec})
			}
			for _, key := range sortedKeys(sl.Consumes) {
				rec, err := records.resolve(svc, sl, "consumes/"+key, sl.Consumes[key])
				if err != nil {
					return nil, err
				}
				ie.Consumes = append(ie.Consumes, invItem{Key: key, Record: rec})
			}
			s := byName[svc]
			if s == nil {
				s = &invService{Service: svc}
				byName[svc] = s
			}
			s.Envs = append(s.Envs, ie)
		}
	}
	inv := make([]invService, 0, len(byName))
	for _, name := range sortedKeys(byName) {
		inv = append(inv, *byName[name])
	}
	return inv, nil
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
