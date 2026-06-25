package diff

import (
	"slices"
	"strings"

	"github.com/wirefit/wirefit/internal/ir"
)

type stepKind byte

const (
	stepField    stepKind = 'f'
	stepItems    stepKind = 'i'
	stepBranch   stepKind = 'u'
	stepMapValue stepKind = 'm'
)

type step struct {
	kind stepKind
	name string
}

type path []step

func (p path) field(name string) path {
	return append(slices.Clone(p), step{stepField, name})
}
func (p path) items() path {
	return append(slices.Clone(p), step{stepItems, ""})
}
func (p path) branch(tag string) path {
	return append(slices.Clone(p), step{stepBranch, tag})
}
func (p path) mapValue() path {
	return append(slices.Clone(p), step{stepMapValue, ""})
}

// String renders $.a.b[].c, $.pet<dog>.name and $.attrs{} forms.
func (p path) String() string {
	var b strings.Builder
	b.WriteString("$")
	for _, s := range p {
		switch s.kind {
		case stepField:
			b.WriteString("." + s.name)
		case stepItems:
			b.WriteString("[]")
		case stepBranch:
			b.WriteString("<" + s.name + ">")
		case stepMapValue:
			b.WriteString("{}")
		}
	}
	return b.String()
}

// resolve walks a schema along a path; nil means the schema does not
// contain that path (i.e. a consumer does not use it).
func resolve(s *ir.Schema, p path) *ir.Schema {
	cur := s
	for _, st := range p {
		if cur == nil {
			return nil
		}
		switch st.kind {
		case stepField:
			if cur.Properties == nil {
				return nil
			}
			cur = cur.Properties[st.name]
		case stepItems:
			cur = cur.Items
		case stepBranch:
			var found *ir.Schema
			for _, b := range cur.OneOf {
				if b.DiscriminatorValue == st.name {
					found = b
					break
				}
			}
			cur = found
		case stepMapValue:
			cur = cur.MapValue()
		}
	}
	return cur
}
