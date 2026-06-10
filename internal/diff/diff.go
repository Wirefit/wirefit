// Package diff implements the direction-aware semantic diff engine (SPEC §8).
//
// Two entry points:
//   - Self:   before/after of one party's schema (provider PR check),
//     classified against registered counterpart usage projections.
//   - Compat: emitter schema vs parser schema (consumer PR check).
package diff

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Class is the severity of a finding. Order matters: higher is worse.
type Class int

const (
	Neutral Class = iota
	Safe
	Warning
	Breaking
)

var classNames = map[Class]string{
	Neutral: "neutral", Safe: "safe", Warning: "warning", Breaking: "breaking",
}

func (c Class) String() string { return classNames[c] }

func (c Class) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

func (c *Class) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for k, v := range classNames {
		if v == s {
			*c = k
			return nil
		}
	}
	return fmt.Errorf("unknown class %q", s)
}

// Direction of data flow for the interaction under check (SPEC §8).
type Direction string

const (
	// P2C: producer → consumer (responses, published events).
	P2C Direction = "p2c"
	// C2P: consumer → producer (request bodies, consumed commands).
	C2P Direction = "c2p"
)

// ParseDirection accepts manifest-level direction words as well.
func ParseDirection(s string) (Direction, error) {
	switch strings.ToLower(s) {
	case "p2c", "response", "event":
		return P2C, nil
	case "c2p", "request":
		return C2P, nil
	}
	return "", fmt.Errorf("unknown direction %q (want response|event|request|p2c|c2p)", s)
}

// Finding is one classified difference.
type Finding struct {
	Class      Class    `json:"class"`
	Rule       string   `json:"rule"`
	Path       string   `json:"path"`
	Message    string   `json:"message"`
	ConsumedBy []string `json:"consumedBy,omitempty"`
	ColdStart  bool     `json:"coldStart,omitempty"`
	// Overridden: the class was downgraded by a recorded override (PRD 3.4).
	Overridden bool `json:"overridden,omitempty"`
}

// Result aggregates findings for one interaction check.
type Result struct {
	Direction Direction `json:"direction"`
	ColdStart bool      `json:"coldStart,omitempty"`
	Findings  []Finding `json:"findings"`
}

// Max returns the worst class present (Neutral when empty).
func (r *Result) Max() Class {
	m := Neutral
	for _, f := range r.Findings {
		if f.Class > m {
			m = f.Class
		}
	}
	return m
}

// ExitCode implements the CLI contract: 0 ok/warn, 1 breaking (PRD 1.7).
func (r *Result) ExitCode() int {
	if r.Max() == Breaking {
		return 1
	}
	return 0
}

func (r *Result) sort() {
	slices.SortFunc(r.Findings, func(a, b Finding) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := strings.Compare(a.Rule, b.Rule); c != 0 {
			return c
		}
		return strings.Compare(a.Message, b.Message)
	})
}

// kindFamily folds "integer" into "number": integer↔float transitions are
// scalar refinements judged by the ir.Fits table, not structural kind changes.
func kindFamily(k string) string {
	if k == "integer" {
		return "number"
	}
	return k
}

// applyColdStart downgrades breaking findings to warnings when no consumer
// is registered for the interaction (locked decision: warn-only cold start).
func (r *Result) applyColdStart() {
	r.ColdStart = true
	for i := range r.Findings {
		if r.Findings[i].Class == Breaking {
			r.Findings[i].Class = Warning
			r.Findings[i].ColdStart = true
			r.Findings[i].Message += " — cold start: no consumers registered, not enforced"
		}
	}
}
