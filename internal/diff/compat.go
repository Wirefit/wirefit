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
	w := &compatWalker{opts: opts, r: r, sender: "provider", receiver: "consumer"}
	if opts.Direction == P2C {
		w.node(path{}, provider, consumer)
	} else {
		w.sender, w.receiver = w.receiver, w.sender
		w.node(path{}, consumer, provider)
	}
	r.sort()
	return r
}

type compatWalker struct {
	opts CompatOptions
	r    *Result
	// sender and receiver name the parties in messages; which one emits
	// depends on the direction.
	sender, receiver string
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
		w.add(Breaking, "type-mismatch", p, fmt.Sprintf(
			"%s sends %s here, %s expects %s", w.sender, article(ke), w.receiver, article(kp)))
		return
	}

	if emitter.Nullable && !parser.Nullable {
		w.add(Breaking, "nullability-mismatch", p, fmt.Sprintf("%s may send null here, %s does not accept null", w.sender, w.receiver))
	}

	if emitter.Scalar != "" && parser.Scalar != "" && emitter.Scalar != parser.Scalar {
		switch ir.Fits(emitter.Scalar, parser.Scalar) {
		case ir.FitLossy: // SPEC F7: the value crosses the wire but not intact.
			w.add(Warning, "scalar-lossy", p, fmt.Sprintf(
				"%s sends %s, %s reads it as %s; large values lose precision", w.sender, emitter.Scalar, w.receiver, parser.Scalar))
		case ir.FitNo:
			w.add(Breaking, "scalar-mismatch", p, fmt.Sprintf(
				"%s sends %s, %s expects %s", w.sender, emitter.Scalar, w.receiver, parser.Scalar))
		}
	}

	// Enum coverage: every value the emitter may produce must be known to the parser.
	if len(parser.Enum) > 0 {
		if len(emitter.Enum) == 0 {
			w.add(Breaking, "enum-open-vs-closed", p,
				fmt.Sprintf("%s may send any value here, %s only accepts a fixed list", w.sender, w.receiver))
		} else {
			for _, v := range emitter.Enum {
				if !contains(parser.Enum, v) {
					w.add(Breaking, "enum-unknown-value", p, fmt.Sprintf(
						"%s may send %q, which the %s does not accept", w.sender, v, w.receiver))
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
				w.add(Breaking, "field-missing", fp, fmt.Sprintf("%s requires this field, %s never sends it", w.receiver, w.sender))
			}
			// Optional expectation on a never-emitted field: tolerated.
			continue
		}
		if parser.IsRequired(name) && !emitter.IsRequired(name) {
			w.add(Breaking, "presence-not-guaranteed", fp, fmt.Sprintf("%s requires this field, %s may leave it out", w.receiver, w.sender))
		}
		w.node(fp, ef, parser.Properties[name])
	}
	if w.opts.StrictParser {
		parserOpen := parser.AdditionalProperties != nil
		if !parserOpen {
			for _, name := range sortedKeys(emitter.Properties) {
				if parser.Properties[name] == nil {
					w.add(Breaking, "unknown-field-rejected", p.field(name),
						fmt.Sprintf("%s sends this field, %s rejects fields it does not know", w.sender, w.receiver))
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
				fmt.Sprintf("%s's map values can be anything, %s expects one fixed type", w.sender, w.receiver))
		case ev != nil && pv != nil:
			w.node(p.mapValue(), ev, pv)
		}
	}
}

func (w *compatWalker) unions(p path, emitter, parser *ir.Schema) {
	if emitter.Discriminator != parser.Discriminator {
		w.add(Breaking, "discriminator-mismatch", p,
			fmt.Sprintf("%s tags this union with %q, %s reads the tag from %q",
				w.sender, emitter.Discriminator, w.receiver, parser.Discriminator))
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
				fmt.Sprintf("%s may send the %q variant, which the %s does not handle", w.sender, eb.DiscriminatorValue, w.receiver))
			continue
		}
		w.node(p.branch(eb.DiscriminatorValue), eb, pb)
	}
}
