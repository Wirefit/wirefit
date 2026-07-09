// Package extract routes manifest dto references to schema extractors.
// Every IR producer (built-in language tools, schema importers, third-party
// commands) implements the same Extractor interface, so the extract command
// carries no language knowledge: registry order is the only routing policy.
package extract

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/wirefit/wirefit/internal/extproto"
)

// Spec is one dto reference with its manifest role: "provided" (the service
// emits this shape) or "consumed" (the service parses it). A ref used on
// both sides yields two specs; each extractor decides what that means.
type Spec struct {
	Ref  string
	Role string
}

// Extractor turns dto references into raw IR documents keyed by ref.
type Extractor interface {
	// Match reports whether ref belongs to this extractor. The first match
	// in registry order wins; a fallback (always true) goes last.
	Match(ref string) bool
	Extract(projectDir string, specs []Spec) (map[string]json.RawMessage, error)
}

// Run dispatches each spec to the first matching extractor in reg and merges
// the results. Each group is sorted by (Ref, Role) and deduped before
// dispatch so identical manifests produce identical extractor invocations
// (NF3) and extractors never see the same spec twice.
func Run(reg []Extractor, projectDir string, specs []Spec) (map[string]json.RawMessage, error) {
	groups := make([][]Spec, len(reg))
	for _, s := range specs {
		i := 0
		for ; i < len(reg); i++ {
			if reg[i].Match(s.Ref) {
				break
			}
		}
		if i == len(reg) {
			return nil, fmt.Errorf("no extractor matches %s; add an extractors entry to contracts.yaml, e.g. %s", s.Ref, routeHint(s.Ref))
		}
		groups[i] = append(groups[i], s)
	}
	out := map[string]json.RawMessage{}
	for i, e := range reg {
		g := groups[i]
		if len(g) == 0 {
			continue
		}
		sort.Slice(g, func(a, b int) bool {
			if g[a].Ref != g[b].Ref {
				return g[a].Ref < g[b].Ref
			}
			return g[a].Role < g[b].Role
		})
		res, err := e.Extract(projectDir, slices.Compact(g))
		if err != nil {
			return nil, err
		}
		maps.Copy(out, res)
	}
	return out, nil
}

// Refs returns the distinct refs in specs, sorted. For extractors whose
// source draws no input/output distinction a ref used on both sides
// extracts once.
func Refs(specs []Spec) []string {
	seen := map[string]bool{}
	refs := make([]string, 0, len(specs))
	for _, s := range specs {
		if !seen[s.Ref] {
			seen[s.Ref] = true
			refs = append(refs, s.Ref)
		}
	}
	sort.Strings(refs)
	return refs
}

// routeHint suggests the manifest extractors entry that would route ref.
// A "file#Type" ref gets its own suffix; anything else (a bare java FQN has
// no selector and its dots are package separators) gets the "*" fallback.
func routeHint(ref string) string {
	file, sel, _ := strings.Cut(ref, "#")
	if ext := path.Ext(file); ext != "" && sel != "" {
		return fmt.Sprintf("{match: %q, command: \"<your-extractor>\"}", ext)
	}
	return `{match: "*", command: "wirefit-java"}`
}

// MatchSuffix reports whether the file part of ref (before any "#Type"
// selector) ends with one of sufs; the suffix "*" matches every ref (the
// manifest fallback rule for suffix-less refs like java FQNs). The one
// suffix rule shared by every suffix-routed extractor: a private copy that
// drifts would silently skew registry routing.
func MatchSuffix(ref string, sufs ...string) bool {
	file, _, _ := strings.Cut(ref, "#")
	for _, s := range sufs {
		if s == "*" || strings.HasSuffix(file, s) {
			return true
		}
	}
	return false
}

// External adapts a manifest-declared third-party extractor command to the
// registry via the extractor protocol (PRD 3.2). Match rules sharing one
// command merge into a single External so the command is invoked once with
// the full spec set.
type External struct {
	Suffixes []string // file suffixes from the manifest match rules, e.g. ".py"
	Command  []string
}

func (x External) Match(ref string) bool { return MatchSuffix(ref, x.Suffixes...) }

func (x External) Extract(projectDir string, specs []Spec) (map[string]json.RawMessage, error) {
	// Pass the specs through verbatim (already sorted and deduped by (Ref,
	// Role) in Run) and let the extractor own the io-semantics decision:
	// whether provided and consumed extraction differ is language knowledge
	// the language-blind core does not hold. A role-sensitive extractor
	// (wirefit-ts: zod defaults differ per side) rejects a ref used on both
	// sides itself; a role-agnostic one (wirefit-java) dedups by ref and
	// extracts once.
	req := extproto.Request{ProjectDir: projectDir}
	for _, s := range specs {
		req.Specs = append(req.Specs, extproto.Spec{Ref: s.Ref, Role: s.Role})
	}
	resp, err := extproto.Invoke(x.Command, req)
	if err != nil {
		return nil, err
	}
	return resp.Schemas, nil
}
