// Package policy implements org-level rule governance (Phase 3 P1): a
// policy.yaml at the root of the contracts repo can re-classify rules
// org-wide and forbid per-service overrides on specific rules. The contracts
// repo's own review process (CODEOWNERS) governs who can change policy.
package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/wirefit/wirefit/internal/diff"
)

type Rule struct {
	// Class re-classifies every finding of this rule ("breaking", "warning",
	// "safe", "neutral"). Empty = keep the engine's classification.
	Class string `yaml:"class"`
	// Overridable=false forbids per-service overrides on this rule.
	Overridable *bool `yaml:"overridable"`
}

type Policy struct {
	Rules map[string]Rule `yaml:"rules"`
}

var classNames = map[string]diff.Class{
	"breaking": diff.Breaking, "warning": diff.Warning, "safe": diff.Safe, "neutral": diff.Neutral,
}

// Load reads policy.yaml from the contracts repo root. Missing file → empty
// policy (everything default, everything overridable).
func Load(contractsRepoDir string) (*Policy, error) {
	p := filepath.Join(contractsRepoDir, "policy.yaml")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Policy{}, nil
	}
	if err != nil {
		return nil, err
	}
	var pol Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&pol); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	for rule, r := range pol.Rules {
		if r.Class != "" {
			if _, ok := classNames[r.Class]; !ok {
				return nil, fmt.Errorf("%s: rule %s: class %q must be breaking|warning|safe|neutral", p, rule, r.Class)
			}
		}
	}
	return &pol, nil
}

// Apply re-classifies findings in place per org policy. Runs after the diff
// engine and BEFORE per-service overrides.
func (p *Policy) Apply(r *diff.Result) {
	if len(p.Rules) == 0 {
		return
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		pr, ok := p.Rules[f.Rule]
		if !ok || pr.Class == "" {
			continue
		}
		want := classNames[pr.Class]
		if f.Class != want {
			f.Message += fmt.Sprintf(" — reclassified %s→%s by org policy", f.Class, want)
			f.Class = want
		}
	}
}

// Overridable reports whether per-service overrides may touch this rule.
func (p *Policy) Overridable(rule string) bool {
	pr, ok := p.Rules[rule]
	if !ok || pr.Overridable == nil {
		return true
	}
	return *pr.Overridable
}
