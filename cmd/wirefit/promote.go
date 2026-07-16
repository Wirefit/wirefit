package main

// The shared candidate-vs-deployed evaluation core behind `can-i-deploy` and
// the promotion view of `matrix`. A candidate is a service's contract set
// proposed for an env; it can come from a local extract (candidateFromIR) or
// from another env's deploy record (candidateFromLock, "promote what runs
// there").

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/manifest"
	"github.com/wirefit/wirefit/internal/store"
)

type candProvide struct {
	id     string
	dir    diff.Direction
	schema *ir.Schema
}

type candConsume struct {
	provider, id string
	schema       *ir.Schema
}

type candidate struct {
	service        string
	rejectsUnknown bool
	provides       []candProvide
	consumes       []candConsume
}

// trackedKind distinguishes the counterpart's evidence: a deploy record in
// the target env, a main-branch fallback, or nothing at all.
type trackedKind int

const (
	tracked       trackedKind = iota // deploy record in the target env
	untrackedMain                    // no record; checked against main-branch state
	untrackedNone                    // no record and nothing published
	unpublished                      // provider's manifest lacks the interaction
)

// deployResult is one candidate-vs-counterpart check. err is set when a
// pinned blob cannot be read (res is nil then).
type deployResult struct {
	side                       string // "provides" | "consumes"
	id                         string
	counterpart                string // provides: the consumer; consumes: the provider
	kind                       trackedKind
	res                        *diff.Result
	err                        error
	consumerBody, providerBody *ir.Schema
}

// dirOf resolves an interaction's direction from the provider's published
// manifest, with ok=false when the provider never published it.
func dirOf(st *store.Store, provider, id string) (diff.Direction, bool) {
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

// strictOf reports a service's published unknown-fields strictness.
func strictOf(st *store.Store, svc string) bool {
	if sm, err := st.ServiceManifest(svc); err == nil && sm != nil {
		return sm.RejectsUnknown()
	}
	return false
}

func sortedLockKeys(lock store.EnvLock) []string {
	keys := make([]string, 0, len(lock))
	for k := range lock {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// candidateFromIR builds the candidate from a local `wirefit extract` run.
// Errors print to stderr and return exit code 2, matching can-i-deploy.
func candidateFromIR(m *manifest.Manifest, irDir string) (candidate, int) {
	c := candidate{service: m.Service, rejectsUnknown: m.RejectsUnknown()}
	for _, p := range m.Provides {
		dir, err := diff.ParseDirection(p.Direction)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit can-i-deploy:", err)
			return c, 2
		}
		sch, err := ir.Load(filepath.Join(irDir, "provides", p.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit can-i-deploy: missing candidate IR for %s; run `wirefit extract` first (%v)\n", p.ID, err)
			return c, 2
		}
		c.provides = append(c.provides, candProvide{id: p.ID, dir: dir, schema: sch})
	}
	for _, cs := range m.Consumes {
		sch, err := ir.Load(filepath.Join(irDir, "consumes", cs.Provider, cs.ID+".ir.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "wirefit can-i-deploy: missing candidate IR for consumed %s/%s (%v)\n", cs.Provider, cs.ID, err)
			return c, 2
		}
		c.consumes = append(c.consumes, candConsume{provider: cs.Provider, id: cs.ID, schema: sch})
	}
	return c, 0
}

// candidateFromLock rebuilds a service's candidate from its deploy record:
// what runs in an env IS the thing being promoted. Directions and strictness
// come from the published manifest, defaulting to P2C like matrixEdges when
// it is unreadable.
func candidateFromLock(st *store.Store, service string, sl *store.ServiceLock) (candidate, error) {
	c := candidate{service: service, rejectsUnknown: strictOf(st, service)}
	sm, _ := st.ServiceManifest(service)
	for _, id := range sortedKeys(sl.Provides) {
		sch, err := st.ReadBlob(sl.Provides[id])
		if err != nil {
			return c, err
		}
		dir := diff.P2C
		if sm != nil {
			for _, p := range sm.Provides {
				if p.ID == id {
					if d, err := diff.ParseDirection(p.Direction); err == nil {
						dir = d
					}
				}
			}
		}
		c.provides = append(c.provides, candProvide{id: id, dir: dir, schema: sch})
	}
	for _, key := range sortedKeys(sl.Consumes) {
		sch, err := st.ReadBlob(sl.Consumes[key])
		if err != nil {
			return c, err
		}
		provider, id, _ := cutString(key, "/")
		c.consumes = append(c.consumes, candConsume{provider: provider, id: id, schema: sch})
	}
	return c, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// evalDeploy checks a candidate against what an env lock says is deployed.
//
// Provider side: does the candidate still satisfy every DEPLOYED consumer?
// Consumers registered at main but never deploy-recorded are checked against
// their main-branch usage instead (never silently green, PRD 4.4).
// Consumer side: does the DEPLOYED provider satisfy the candidate's
// expectations, falling back to the provider's published schema?
func evalDeploy(st *store.Store, c candidate, env string, lock store.EnvLock,
	staleBefore time.Time, staleDays int) ([]deployResult, error) {

	var out []deployResult
	for _, cp := range c.provides {
		// Consumers registered at main, for untracked detection (PRD 4.4).
		mainConsumers, err := st.ConsumersOf(c.service, cp.id)
		if err != nil {
			return nil, err
		}
		for _, svc := range sortedLockKeys(lock) {
			if svc == c.service {
				continue
			}
			sl := lock[svc]
			hash, ok := sl.Consumes[c.service+"/"+cp.id]
			if !ok {
				continue
			}
			delete(mainConsumers, svc)
			dr := deployResult{side: "provides", id: cp.id, counterpart: svc, kind: tracked,
				providerBody: cp.schema}
			proj, err := st.ReadBlob(hash)
			if err != nil {
				dr.err = err
				out = append(out, dr)
				continue
			}
			dr.consumerBody = proj
			r := diff.Compat(cp.schema, proj, diff.CompatOptions{Direction: cp.dir, StrictParser: strictOf(st, svc)})
			if sl.RecordedAt.Before(staleBefore) {
				r.Findings = append(r.Findings, diff.Finding{
					Class: diff.Warning, Rule: "stale-deploy-record", Path: "$",
					Message: fmt.Sprintf("deploy record from %s is older than %d days; re-record to trust this result",
						sl.RecordedAt.Format("2006-01-02"), staleDays),
				})
			}
			dr.res = r
			out = append(out, dr)
		}
		for _, svc := range sortedConsumerKeys(mainConsumers) {
			cons := mainConsumers[svc]
			r := diff.Compat(cp.schema, cons.Schema, diff.CompatOptions{Direction: cp.dir, StrictParser: cons.RejectUnknown})
			r.Findings = append(r.Findings, diff.Finding{
				Class: diff.Warning, Rule: "untracked-consumer", Path: "$",
				Message: fmt.Sprintf("%s has no deploy record in %s; checked against its main-branch usage instead", svc, env),
			})
			out = append(out, deployResult{side: "provides", id: cp.id, counterpart: svc, kind: untrackedMain,
				res: r, consumerBody: cons.Schema, providerBody: cp.schema})
		}
	}

	for _, cc := range c.consumes {
		dr := deployResult{side: "consumes", id: cc.id, counterpart: cc.provider, consumerBody: cc.schema}
		dir, ok := dirOf(st, cc.provider, cc.id)
		if !ok {
			dr.kind = unpublished
			dr.res = &diff.Result{Findings: []diff.Finding{{
				Class: diff.Warning, Rule: "provider-unpublished", Path: "$",
				Message: "provider has not published this interaction",
			}}}
			out = append(out, dr)
			continue
		}
		strict := c.rejectsUnknown
		if dir == diff.C2P {
			strict = strictOf(st, cc.provider)
		}
		var provIR *ir.Schema
		var extra *diff.Finding
		if sl, ok := lock[cc.provider]; ok {
			if hash, ok := sl.Provides[cc.id]; ok {
				var err error
				provIR, err = st.ReadBlob(hash)
				if err != nil {
					dr.err = err
					out = append(out, dr)
					continue
				}
				if sl.RecordedAt.Before(staleBefore) {
					extra = &diff.Finding{Class: diff.Warning, Rule: "stale-deploy-record", Path: "$",
						Message: fmt.Sprintf("provider deploy record from %s is older than %d days", sl.RecordedAt.Format("2006-01-02"), staleDays)}
				}
			}
		}
		if provIR == nil {
			var ok bool
			var err error
			provIR, ok, err = st.ProviderIR(cc.provider, cc.id)
			if err != nil || !ok {
				dr.kind = untrackedNone
				dr.res = &diff.Result{Findings: []diff.Finding{{
					Class: diff.Warning, Rule: "untracked-provider", Path: "$",
					Message: "provider has neither a deploy record nor a published schema",
				}}}
				out = append(out, dr)
				continue
			}
			dr.kind = untrackedMain
			extra = &diff.Finding{Class: diff.Warning, Rule: "untracked-provider", Path: "$",
				Message: fmt.Sprintf("%s has no deploy record in %s; checked against its main-branch schema instead", cc.provider, env)}
		}
		dr.providerBody = provIR
		r := diff.Compat(provIR, cc.schema, diff.CompatOptions{Direction: dir, StrictParser: strict})
		if extra != nil {
			r.Findings = append(r.Findings, *extra)
		}
		dr.res = r
		out = append(out, dr)
	}
	return out, nil
}

func sortedConsumerKeys(m map[string]diff.Consumer) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// promoEdge is one candidate-vs-deployed check for promoting a service from
// one env into the next; the field names are the `--format json` contract.
type promoEdge struct {
	From, To, Service, Side, Counterpart, Interaction string
	Status                                            matrixStatus
	Detail                                            string
	InSync                                            bool           `json:",omitempty"`
	Findings                                          []diff.Finding `json:",omitempty"`
	// Each row is one interaction even though the service candidate spans many
	// blobs, so the HTML detail view can carry the exact compared pair.
	ConsumerRecord, ProviderRecord *deployRecord `json:"-"`
	ConsumerBody, ProviderBody     *ir.Schema    `json:"-"`
}

// promoCheck names the single check a promotion row performed, mirroring the
// can-i-deploy labels. Empty for the per-service in-sync rollup row.
func promoCheck(e promoEdge) string {
	switch e.Side {
	case "provides":
		return fmt.Sprintf("provides %s ⇐ %s", e.Interaction, e.Counterpart)
	case "consumes":
		return fmt.Sprintf("consumes %s/%s", e.Counterpart, e.Interaction)
	}
	return ""
}

func promoParties(e promoEdge) (consumer, provider string) {
	if e.Side == "provides" {
		return e.Counterpart, e.Service
	}
	return e.Service, e.Counterpart
}

func attachPromoRecords(records *deployRecordResolver, e *promoEdge, source *store.ServiceLock, target store.EnvLock) error {
	consumer, provider := promoParties(*e)
	var err error
	switch e.Side {
	case "provides":
		hash := source.Provides[e.Interaction]
		e.ProviderRecord, err = records.resolve(provider, source, "provides/"+e.Interaction, hash)
		if err != nil {
			return err
		}
		if sl := target[consumer]; sl != nil {
			key := provider + "/" + e.Interaction
			e.ConsumerRecord, err = records.resolve(consumer, sl, "consumes/"+key, sl.Consumes[key])
		}
	case "consumes":
		key := provider + "/" + e.Interaction
		e.ConsumerRecord, err = records.resolve(consumer, source, "consumes/"+key, source.Consumes[key])
		if err != nil {
			return err
		}
		if sl := target[provider]; sl != nil {
			e.ProviderRecord, err = records.resolve(provider, sl, "provides/"+e.Interaction, sl.Provides[e.Interaction])
		}
	}
	return err
}

func inSyncDetail(to, detail string) string {
	base := "in sync: the same contracts are already deployed in " + to
	if detail == "" {
		return base
	}
	return trimJoin(base, detail)
}

func allPromoChecksOK(edges []promoEdge) bool {
	for _, e := range edges {
		if e.Status != matrixStatusOK {
			return false
		}
	}
	return true
}

// promoEdges answers, for each adjacent pipeline pair (A, B) and each service
// recorded in A: would what runs in A be compatible if deployed to B? The
// candidate is A's recorded state, evaluated exactly like can-i-deploy.
func promoEdges(st *store.Store, pipeline []string, staleBefore time.Time, staleDays int) ([]promoEdge, error) {
	var edges []promoEdge
	records := newDeployRecordResolver(st)
	for i := 0; i+1 < len(pipeline); i++ {
		from, to := pipeline[i], pipeline[i+1]
		lockA, err := st.LoadEnvLock(from)
		if err != nil {
			return nil, err
		}
		lockB, err := st.LoadEnvLock(to)
		if err != nil {
			return nil, err
		}
		var chunk []promoEdge
		for _, svc := range sortedLockKeys(lockA) {
			sl := lockA[svc]
			slB, recordedInTarget := lockB[svc]
			inSync := recordedInTarget && maps.Equal(sl.Provides, slB.Provides) && maps.Equal(sl.Consumes, slB.Consumes)
			stale := sl.RecordedAt.Before(staleBefore)
			cand, err := candidateFromLock(st, svc, sl)
			if err != nil {
				e := promoEdge{From: from, To: to, Service: svc,
					Status: matrixStatusError, Detail: "missing blob; re-publish + re-record", InSync: inSync}
				if stale {
					e.Detail = trimJoin(e.Detail, "stale deploy record")
				}
				if inSync {
					e.Detail = inSyncDetail(to, e.Detail)
				}
				chunk = append(chunk, e)
				continue
			}
			drs, err := evalDeploy(st, cand, to, lockB, staleBefore, staleDays)
			if err != nil {
				return nil, err
			}
			var serviceEdges []promoEdge
			for _, dr := range drs {
				e := promoEdge{From: from, To: to, Service: svc,
					Side: dr.side, Counterpart: dr.counterpart, Interaction: dr.id, InSync: inSync,
					ConsumerBody: dr.consumerBody, ProviderBody: dr.providerBody}
				if dr.res != nil {
					e.Findings = dr.res.Findings
				}
				if err := attachPromoRecords(records, &e, sl, lockB); err != nil {
					return nil, err
				}
				switch {
				case dr.err != nil:
					e.Status, e.Detail = matrixStatusError, "missing blob; re-publish + re-record"
				case dr.kind != tracked:
					// The untracked/unpublished finding is appended last;
					// the detail explains why no reliable answer exists.
					e.Status = matrixStatusUntracked
					e.Detail = dr.res.Findings[len(dr.res.Findings)-1].Message
				default:
					switch dr.res.Max() {
					case diff.Breaking:
						e.Status, e.Detail = matrixStatusIncompatible, dr.res.Findings[0].Message
					case diff.Warning:
						e.Status, e.Detail = matrixStatusWarning, dr.res.Findings[0].Message
					default:
						e.Status = matrixStatusOK
					}
				}
				if stale {
					e.Detail = trimJoin(e.Detail, "stale deploy record")
				}
				if inSync {
					e.Detail = inSyncDetail(to, e.Detail)
				}
				serviceEdges = append(serviceEdges, e)
			}
			if inSync && allPromoChecksOK(serviceEdges) {
				chunk = append(chunk, promoEdge{From: from, To: to, Service: svc, Status: matrixStatusOK,
					Detail: inSyncDetail(to, ""), InSync: true})
				continue
			}
			chunk = append(chunk, serviceEdges...)
		}
		// Sort within the pair: the pairs themselves keep pipeline order.
		sort.Slice(chunk, func(i, j int) bool {
			a, b := chunk[i], chunk[j]
			if a.Service != b.Service {
				return a.Service < b.Service
			}
			if a.Side != b.Side {
				return a.Side < b.Side
			}
			if a.Counterpart != b.Counterpart {
				return a.Counterpart < b.Counterpart
			}
			return a.Interaction < b.Interaction
		})
		edges = append(edges, chunk...)
	}
	return edges, nil
}

// canIDeployLabel reproduces the historical can-i-deploy result keys, which
// are part of the `--format json` output.
func canIDeployLabel(dr deployResult, env string) string {
	if dr.side == "provides" {
		if dr.kind == tracked {
			return fmt.Sprintf("provides %s ⇐ %s@%s", dr.id, dr.counterpart, env)
		}
		return fmt.Sprintf("provides %s ⇐ %s (untracked)", dr.id, dr.counterpart)
	}
	switch dr.kind {
	case unpublished:
		return "consumes " + dr.counterpart + "/" + dr.id
	case untrackedMain:
		return fmt.Sprintf("consumes %s/%s (untracked)", dr.counterpart, dr.id)
	}
	return fmt.Sprintf("consumes %s/%s @%s", dr.counterpart, dr.id, env)
}
