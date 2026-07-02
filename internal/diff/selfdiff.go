package diff

import (
	"fmt"
	"sort"

	"github.com/wirefit/wirefit/internal/ir"
)

// Consumer is one registered counterpart's usage projection for the
// interaction under check (P2C: fields it reads; C2P: fields it sends).
type Consumer struct {
	Schema        *ir.Schema `json:"schema"`
	RejectUnknown bool       `json:"rejectUnknown,omitempty"`
}

// SelfOptions configures a before/after check of one party's schema.
type SelfOptions struct {
	Direction Direction
	Consumers map[string]Consumer
	// ProviderRejectsUnknown: C2P only — the provider's deserializer rejects
	// unknown request fields (flips field-removed to breaking, SPEC C5).
	ProviderRejectsUnknown bool
	// ColdStart: no consumers registered for this interaction at all —
	// breaking findings are downgraded to warnings (locked decision).
	ColdStart bool
}

// Self classifies the changes between two versions of the same party's
// schema, direction-aware per SPEC §8.
//
// The guiding symmetry: in P2C, changes that WIDEN the emitted value set
// break consumers; in C2P, changes that NARROW the accepted value set break
// consumers. P2C breaking findings on paths no registered consumer reads are
// downgraded to safe — the consumer-usage payoff that schema registries
// cannot offer.
func Self(before, after *ir.Schema, opts SelfOptions) *Result {
	r := &Result{Direction: opts.Direction, Findings: []Finding{}}
	w := &selfWalker{opts: opts, r: r}
	w.node(path{}, before, after)
	if opts.ColdStart {
		r.applyColdStart()
	}
	r.sort()
	return r
}

type selfWalker struct {
	opts SelfOptions
	r    *Result
}

// consumedBy lists consumers whose projection contains the path.
func (w *selfWalker) consumedBy(p path) []string {
	var out []string
	for name, c := range w.opts.Consumers {
		if resolve(c.Schema, p) != nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// consumersLacking lists consumers whose projection does NOT contain the path.
func (w *selfWalker) consumersLacking(p path) []string {
	var out []string
	for name, c := range w.opts.Consumers {
		if resolve(c.Schema, p) == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// add records a finding. For P2C, breaking/warning findings on paths that no
// registered consumer reads are downgraded to safe (when consumers exist).
func (w *selfWalker) add(class Class, rule string, p path, msg string) {
	f := Finding{Class: class, Rule: rule, Path: p.String(), Message: msg}
	if w.opts.Direction == P2C && class >= Warning && len(w.opts.Consumers) > 0 {
		consumed := w.consumedBy(p)
		if len(consumed) == 0 {
			f.Class = Safe
			f.Message = msg + " (no registered consumer reads this path)"
		} else {
			f.ConsumedBy = consumed
		}
	}
	w.r.Findings = append(w.r.Findings, f)
}

// addRaw records a finding exempt from the consumed-path downgrade
// (used where the affected consumers are computed by the rule itself).
func (w *selfWalker) addRaw(class Class, rule string, p path, msg string, consumers []string) {
	w.r.Findings = append(w.r.Findings, Finding{
		Class: class, Rule: rule, Path: p.String(), Message: msg, ConsumedBy: consumers,
	})
}

// widen: the emitted value set grows → breaks parsers in P2C, safe in C2P.
func (w *selfWalker) widen(rule string, p path, msg string) {
	if w.opts.Direction == P2C {
		w.add(Breaking, rule, p, msg)
	} else {
		w.add(Safe, rule, p, msg)
	}
}

// narrow: the accepted value set shrinks → breaks senders in C2P, safe in P2C.
func (w *selfWalker) narrow(rule string, p path, msg string) {
	if w.opts.Direction == C2P {
		w.add(Breaking, rule, p, msg)
	} else {
		w.add(Safe, rule, p, msg)
	}
}

func (w *selfWalker) node(p path, b, a *ir.Schema) {
	if b == nil || a == nil {
		return // structural presence is handled at the parent
	}
	if b.Recursive || a.Recursive {
		if b.Recursive != a.Recursive {
			w.add(Breaking, "type-changed", p, "recursive marker changed")
		}
		return
	}

	kb, ka := b.JSONKind(), a.JSONKind()
	if kindFamily(kb) != kindFamily(ka) {
		w.add(Breaking, "type-changed", p, fmt.Sprintf("type changed: %s → %s", kb, ka))
		return // children are meaningless across a kind change
	}

	// Nullability (distinct from absence, SPEC §7).
	if !b.Nullable && a.Nullable {
		w.widen("nullable-added", p, "value may now be null")
	}
	if b.Nullable && !a.Nullable {
		w.narrow("nullable-removed", p, "null no longer allowed")
	}

	// Scalar refinement within the same JSON kind.
	if b.Scalar != "" && a.Scalar != "" && b.Scalar != a.Scalar {
		var fit ir.Fit
		if w.opts.Direction == P2C {
			fit = ir.Fits(a.Scalar, b.Scalar) // new emissions parsed as old type
		} else {
			fit = ir.Fits(b.Scalar, a.Scalar) // old submissions parsed as new type
		}
		msg := fmt.Sprintf("scalar changed: %s → %s", b.Scalar, a.Scalar)
		switch fit {
		case ir.FitOK:
			w.add(Safe, "scalar-changed", p, msg)
		case ir.FitLossy:
			w.add(Warning, "scalar-lossy", p, msg+" (precision loss possible)")
		case ir.FitNo:
			w.add(Breaking, "scalar-changed", p, msg)
		}
	}

	// Enums (string-valued in v1).
	w.enums(p, b, a)

	// Object properties.
	if kb == "object" {
		w.objects(p, b, a)
	}

	// Array items.
	if kb == "array" {
		w.node(p.items(), b.Items, a.Items)
	}

	// Tagged unions.
	if kb == "union" {
		w.unions(p, b, a)
	}
}

func (w *selfWalker) enums(p path, b, a *ir.Schema) {
	if len(b.Enum) == 0 && len(a.Enum) == 0 {
		return
	}
	switch {
	case len(b.Enum) == 0:
		w.narrow("enum-restricted", p, "previously open value is now a closed enum")
	case len(a.Enum) == 0:
		w.widen("enum-opened", p, "previously closed enum is now an open value")
	default:
		for _, v := range a.Enum {
			if !contains(b.Enum, v) {
				w.widen("enum-value-added", p, fmt.Sprintf("enum value %q added", v))
			}
		}
		for _, v := range b.Enum {
			if !contains(a.Enum, v) {
				// Conservative in C2P: we cannot know which values consumers
				// send unless their projections carry enums (future refinement).
				w.narrow("enum-value-removed", p, fmt.Sprintf("enum value %q removed", v))
			}
		}
	}
}

func (w *selfWalker) objects(p path, b, a *ir.Schema) {
	// Open-map flips.
	bOpen := b.AdditionalProperties != nil
	aOpen := a.AdditionalProperties != nil
	if !bOpen && aOpen {
		w.widen("additional-properties-opened", p, "object now allows arbitrary additional properties")
	}
	if bOpen && !aOpen {
		w.narrow("additional-properties-closed", p, "object no longer allows additional properties")
	}
	// Map value type changes (both still open maps): constraining the value type
	// narrows, opening it widens — mirroring enum open/closed.
	if bOpen && aOpen {
		bv, av := b.MapValue(), a.MapValue()
		switch {
		case bv == nil && av != nil:
			w.narrow("map-value-restricted", p.mapValue(), "map values were unconstrained, now a fixed type")
		case bv != nil && av == nil:
			w.widen("map-value-opened", p.mapValue(), "map values were a fixed type, now unconstrained")
		case bv != nil && av != nil:
			w.node(p.mapValue(), bv, av)
		}
	}

	for _, name := range sortedKeys(b.Properties) {
		fp := p.field(name)
		if a.Properties[name] == nil {
			w.fieldRemoved(fp)
			continue
		}
		w.requiredTransition(fp, b.IsRequired(name), a.IsRequired(name))
		w.node(fp, b.Properties[name], a.Properties[name])
	}
	for _, name := range sortedKeys(a.Properties) {
		if b.Properties[name] == nil {
			w.fieldAdded(p.field(name), a.IsRequired(name))
		}
	}
}

func (w *selfWalker) fieldRemoved(fp path) {
	if w.opts.Direction == P2C {
		// Breaking iff some registered consumer reads it; add() handles the
		// unconsumed downgrade and attaches consumed-by.
		w.add(Breaking, "field-removed", fp, "field removed")
		return
	}
	// C2P: field no longer in the accepted set. Senders that still include it
	// are only broken if the provider rejects unknown fields.
	if w.opts.ProviderRejectsUnknown {
		w.addRaw(Breaking, "field-removed", fp,
			"field removed and provider rejects unknown fields", w.consumedBy(fp))
	} else {
		w.add(Safe, "field-removed", fp, "field removed from accepted set (unknown fields ignored)")
	}
}

func (w *selfWalker) fieldAdded(fp path, required bool) {
	if w.opts.Direction == P2C {
		// Safe, except for consumers whose deserializer rejects unknown fields.
		var strict []string
		for name, c := range w.opts.Consumers {
			if c.RejectUnknown {
				strict = append(strict, name)
			}
		}
		sort.Strings(strict)
		if len(strict) > 0 {
			w.addRaw(Breaking, "field-added", fp,
				"field added but some consumers reject unknown fields", strict)
		} else {
			w.add(Safe, "field-added", fp, "field added")
		}
		return
	}
	// C2P: a new required field breaks every consumer not already sending it.
	if required {
		lacking := w.consumersLacking(fp)
		if len(w.opts.Consumers) > 0 && len(lacking) == 0 {
			w.addRaw(Safe, "required-field-added", fp,
				"required field added; all registered consumers already send it", nil)
		} else {
			w.addRaw(Breaking, "required-field-added", fp,
				"required field added; existing consumers do not send it", lacking)
		}
		return
	}
	w.add(Safe, "field-added", fp, "optional field added")
}

func (w *selfWalker) requiredTransition(fp path, wasRequired, isRequired bool) {
	switch {
	case wasRequired && !isRequired:
		// Presence no longer guaranteed → widen.
		w.widen("required-to-optional", fp, "field is no longer guaranteed to be present")
	case !wasRequired && isRequired:
		if w.opts.Direction == C2P {
			// Breaks exactly the consumers that do not already send the field.
			lacking := w.consumersLacking(fp)
			if len(w.opts.Consumers) > 0 && len(lacking) == 0 {
				w.addRaw(Safe, "optional-to-required", fp,
					"field now required; all registered consumers already send it", nil)
			} else {
				w.addRaw(Breaking, "optional-to-required", fp,
					"field now required; consumers may not send it", lacking)
			}
			return
		}
		w.add(Safe, "optional-to-required", fp, "field is now guaranteed to be present")
	}
}

func (w *selfWalker) unions(p path, b, a *ir.Schema) {
	if b.Discriminator != a.Discriminator {
		w.add(Breaking, "discriminator-changed", p,
			fmt.Sprintf("discriminator changed: %s → %s", b.Discriminator, a.Discriminator))
		return
	}
	branches := func(s *ir.Schema) map[string]*ir.Schema {
		m := map[string]*ir.Schema{}
		for _, br := range s.OneOf {
			m[br.DiscriminatorValue] = br
		}
		return m
	}
	bb, ab := branches(b), branches(a)
	for _, tag := range sortedKeys(bb) {
		bp := p.branch(tag)
		if ab[tag] == nil {
			w.narrow("union-branch-removed", bp, fmt.Sprintf("union branch %q removed", tag))
			continue
		}
		w.node(bp, bb[tag], ab[tag])
	}
	for _, tag := range sortedKeys(ab) {
		if bb[tag] == nil {
			// Reported at the union node, not the new branch path: consumers'
			// projections contain the union, never the branch they don't know.
			w.widen("union-branch-added", p, fmt.Sprintf("union branch %q added", tag))
		}
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
