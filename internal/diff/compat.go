package diff

import (
	"fmt"

	"github.com/wirefit/wirefit/internal/ir"
)

// CompatOptions configures an emitter-vs-parser compatibility check —
// used on consumer PRs: does the counterpart's published schema satisfy
// what this consumer expects (P2C), or does what this consumer sends
// satisfy the provider (C2P)?
type CompatOptions struct {
	Direction Direction
	// StrictParser: the parsing side rejects unknown fields.
	StrictParser bool
}

// Compat checks a provider schema against one consumer schema.
// P2C: provider emits, consumer parses. C2P: consumer emits, provider parses.
func Compat(provider, consumer *ir.Schema, opts CompatOptions) *Result {
	r := &Result{Direction: opts.Direction, Findings: []Finding{}}
	w := &compatWalker{opts: opts, r: r}
	if opts.Direction == P2C {
		w.node(path{}, provider, consumer)
	} else {
		w.node(path{}, consumer, provider)
	}
	r.sort()
	return r
}

type compatWalker struct {
	opts CompatOptions
	r    *Result
}

func (w *compatWalker) add(class Class, rule string, p path, msg string) {
	w.r.Findings = append(w.r.Findings, Finding{Class: class, Rule: rule, Path: p.String(), Message: msg})
}

// node checks that everything `parser` relies on is guaranteed by `emitter`,
// and that everything `emitter` may produce is parseable.
func (w *compatWalker) node(p path, emitter, parser *ir.Schema) {
	if emitter == nil || parser == nil {
		return
	}
	if emitter.Recursive || parser.Recursive {
		return // recursion cut-off: checked up to the marker
	}

	ke, kp := emitter.JSONKind(), parser.JSONKind()
	if kindFamily(ke) != kindFamily(kp) {
		w.add(Breaking, "type-mismatch", p, fmt.Sprintf("emitted %s, parsed as %s", ke, kp))
		return
	}

	if emitter.Nullable && !parser.Nullable {
		w.add(Breaking, "nullability-mismatch", p, "value may be null but parser does not accept null")
	}

	if emitter.Scalar != "" && parser.Scalar != "" && emitter.Scalar != parser.Scalar {
		msg := fmt.Sprintf("emitted %s, parsed as %s", emitter.Scalar, parser.Scalar)
		switch ir.Fits(emitter.Scalar, parser.Scalar) {
		case ir.FitLossy:
			w.add(Warning, "scalar-lossy", p, msg+" (precision loss possible — SPEC F7)")
		case ir.FitNo:
			w.add(Breaking, "scalar-mismatch", p, msg)
		}
	}

	// Enum coverage: every value the emitter may produce must be known to the parser.
	if len(parser.Enum) > 0 {
		if len(emitter.Enum) == 0 {
			w.add(Breaking, "enum-open-vs-closed", p, "emitter value is unconstrained but parser accepts a closed enum")
		} else {
			for _, v := range emitter.Enum {
				if !contains(parser.Enum, v) {
					w.add(Breaking, "enum-unknown-value", p, fmt.Sprintf("emitter may produce %q, unknown to parser", v))
				}
			}
		}
	}

	switch ke {
	case "object":
		w.objects(p, emitter, parser)
	case "array":
		w.node(p.items(), emitter.Items, parser.Items)
	case "union":
		w.unions(p, emitter, parser)
	}
}

func (w *compatWalker) objects(p path, emitter, parser *ir.Schema) {
	for _, name := range sortedKeys(parser.Properties) {
		fp := p.field(name)
		ef := emitter.Properties[name]
		if ef == nil {
			if parser.IsRequired(name) {
				w.add(Breaking, "field-missing", fp, "parser requires field the emitter does not provide")
			}
			// Optional expectation on a never-emitted field: tolerated.
			continue
		}
		if parser.IsRequired(name) && !emitter.IsRequired(name) {
			w.add(Breaking, "presence-not-guaranteed", fp, "parser requires field but emitter may omit it")
		}
		w.node(fp, ef, parser.Properties[name])
	}
	if w.opts.StrictParser {
		parserOpen := parser.AdditionalProperties != nil
		if !parserOpen {
			for _, name := range sortedKeys(emitter.Properties) {
				if parser.Properties[name] == nil {
					w.add(Breaking, "unknown-field-rejected", p.field(name),
						"emitter sends field unknown to a strict parser")
				}
			}
		}
	}

	// Map value compatibility: an unexpressed emitter value type against a parser
	// expecting a fixed one is unsafe (mirrors enum-open-vs-closed).
	if emitter.AdditionalProperties != nil && parser.AdditionalProperties != nil {
		ev, pv := emitter.MapValue(), parser.MapValue()
		switch {
		case ev == nil && pv != nil:
			w.add(Breaking, "map-value-open-vs-typed", p.mapValue(),
				"emitter map values are unconstrained but parser expects a fixed value type")
		case ev != nil && pv != nil:
			w.node(p.mapValue(), ev, pv)
		}
	}
}

func (w *compatWalker) unions(p path, emitter, parser *ir.Schema) {
	if emitter.Discriminator != parser.Discriminator {
		w.add(Breaking, "discriminator-mismatch", p,
			fmt.Sprintf("discriminators differ: %s vs %s", emitter.Discriminator, parser.Discriminator))
		return
	}
	parserBranches := map[string]*ir.Schema{}
	for _, b := range parser.OneOf {
		parserBranches[b.DiscriminatorValue] = b
	}
	for _, eb := range emitter.OneOf {
		pb := parserBranches[eb.DiscriminatorValue]
		if pb == nil {
			w.add(Breaking, "union-branch-unknown", p.branch(eb.DiscriminatorValue),
				fmt.Sprintf("emitter may produce variant %q, unknown to parser", eb.DiscriminatorValue))
			continue
		}
		w.node(p.branch(eb.DiscriminatorValue), eb, pb)
	}
}
