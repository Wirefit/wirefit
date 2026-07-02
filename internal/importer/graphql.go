package importer

// GraphQL importer (PRD 5.3/5.4).
//
// Type mode  (schema.graphql#TypeName): SDL object/input types → IR.
//   Output types: selected fields are always present in a response → required;
//   nullable unless `!`. Input types: non-null without default → required.
//   Unions/interfaces → oneOf discriminated by __typename.
//
// Operation mode (getOrder.graphql, no selector): persisted queries are the
// EXACT consumer usage — the best consumer-side input wirefit can get. The
// selection set is resolved against settings.graphql-schema and projected to
// an IR usage document rooted at the operation's first root field.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

var gqlScalars = map[string]string{
	"String": "string", "ID": "string", "Boolean": "bool",
	"Int": "int32", "Float": "float64",
	// Common custom scalars by convention; anything else fails loudly.
	"DateTime": "datetime", "Date": "date", "UUID": "uuid",
}

func loadSDL(projectDir, file string) (*ast.Schema, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, file))
	if err != nil {
		return nil, err
	}
	schema, gerr := gqlparser.LoadSchema(&ast.Source{Name: file, Input: string(data)})
	if gerr != nil {
		return nil, fmt.Errorf("%s: %v", file, gerr)
	}
	return schema, nil
}

func importGraphQLType(projectDir, file, typeName string) (json.RawMessage, error) {
	schema, err := loadSDL(projectDir, file)
	if err != nil {
		return nil, err
	}
	def := schema.Types[typeName]
	if def == nil {
		return nil, fmt.Errorf("%s: type %q not found", file, typeName)
	}
	n, err := gqlType(schema, def, nil, file+"#"+typeName)
	if err != nil {
		return nil, err
	}
	return marshal(n)
}

func gqlType(schema *ast.Schema, def *ast.Definition, stack []string, ctx string) (node, error) {
	for _, s := range stack {
		if s == def.Name {
			return node{"x-ct-recursive": true}, nil
		}
	}
	stack = append(stack, def.Name)

	switch def.Kind {
	case ast.Enum:
		var vals []string
		for _, v := range def.EnumValues {
			vals = append(vals, v.Name)
		}
		sort.Strings(vals)
		n := scalarNode("string")
		n["enum"] = vals
		return n, nil

	case ast.Object, ast.InputObject:
		props := node{}
		var required []string
		for _, f := range def.Fields {
			// __typename is introspection metadata, not data; a parameterized
			// field on an object is an interaction of its own, not a data field.
			if f.Name == "__typename" || (len(f.Arguments) > 0 && def.Kind == ast.Object) {
				continue
			}
			fn, err := gqlFieldType(schema, f.Type, stack, ctx+"."+f.Name)
			if err != nil {
				return nil, err
			}
			props[f.Name] = fn
			if def.Kind == ast.InputObject {
				// Inputs: non-null without default must be sent.
				if f.Type.NonNull && f.DefaultValue == nil {
					required = append(required, f.Name)
				}
			} else {
				// Outputs: selected fields are always present (maybe null).
				required = append(required, f.Name)
			}
		}
		if len(props) == 0 {
			return nil, fmt.Errorf("type with no plain data fields at %s", ctx)
		}
		out := node{"type": "object", "properties": props}
		if len(required) > 0 {
			sort.Strings(required)
			out["required"] = required
		}
		return out, nil

	case ast.Union, ast.Interface:
		var members []*ast.Definition
		for _, t := range schema.PossibleTypes[def.Name] {
			members = append(members, t)
		}
		if len(members) == 0 {
			return nil, fmt.Errorf("union/interface %s has no possible types at %s", def.Name, ctx)
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		var branches []any
		for _, m := range members {
			bn, err := gqlType(schema, m, stack, ctx+"<"+m.Name+">")
			if err != nil {
				return nil, err
			}
			bn["x-ct-discriminator-value"] = m.Name
			branches = append(branches, bn)
		}
		return node{"x-ct-discriminator": "__typename", "oneOf": branches}, nil

	case ast.Scalar:
		if s, ok := gqlScalars[def.Name]; ok {
			return scalarNode(s), nil
		}
		return nil, fmt.Errorf("custom scalar %q at %s — no canonical mapping (map it to a standard scalar in the SDL or extend gqlScalars)", def.Name, ctx)
	}
	return nil, fmt.Errorf("unsupported GraphQL kind %s at %s", def.Kind, ctx)
}

func gqlFieldType(schema *ast.Schema, t *ast.Type, stack []string, ctx string) (node, error) {
	if t.Elem != nil { // list
		inner, err := gqlFieldType(schema, t.Elem, stack, ctx+"[]")
		if err != nil {
			return nil, err
		}
		n := node{"type": "array", "items": inner}
		if !t.NonNull {
			n["x-ct-nullable"] = true
		}
		return n, nil
	}
	def := schema.Types[t.NamedType]
	if def == nil {
		return nil, fmt.Errorf("unknown type %s at %s", t.NamedType, ctx)
	}
	n, err := gqlType(schema, def, stack, ctx)
	if err != nil {
		return nil, err
	}
	if !t.NonNull {
		n["x-ct-nullable"] = true
	}
	return n, nil
}

// --- operation mode (PRD 5.4) ----------------------------------------------

func importGraphQLOperation(projectDir, file, sdlPath string) (json.RawMessage, error) {
	if sdlPath == "" {
		return nil, fmt.Errorf("%s: operation files need settings.graphql-schema pointing at the provider SDL", file)
	}
	schema, err := loadSDL(projectDir, sdlPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(projectDir, file))
	if err != nil {
		return nil, err
	}
	doc, gerr := gqlparser.LoadQuery(schema, string(data))
	if gerr != nil {
		return nil, fmt.Errorf("%s: %v", file, gerr)
	}
	if len(doc.Operations) != 1 {
		return nil, fmt.Errorf("%s: exactly one operation per file (found %d)", file, len(doc.Operations))
	}
	op := doc.Operations[0]
	if len(op.SelectionSet) != 1 {
		return nil, fmt.Errorf("%s: exactly one root field per operation in v1 (found %d) — split the file", file, len(op.SelectionSet))
	}
	rootField, ok := op.SelectionSet[0].(*ast.Field)
	if !ok {
		return nil, fmt.Errorf("%s: root selection must be a plain field", file)
	}
	n, err := projectSelection(schema, rootField.Definition.Type, rootField.SelectionSet, file+"@"+rootField.Name)
	if err != nil {
		return nil, err
	}
	return marshal(n)
}

// projectSelection builds the usage projection: exactly the fields this
// operation reads, with types/nullability from the SDL.
func projectSelection(schema *ast.Schema, t *ast.Type, sel ast.SelectionSet, ctx string) (node, error) {
	if t.Elem != nil {
		inner, err := projectSelection(schema, t.Elem, sel, ctx+"[]")
		if err != nil {
			return nil, err
		}
		n := node{"type": "array", "items": inner}
		if !t.NonNull {
			n["x-ct-nullable"] = true
		}
		return n, nil
	}
	def := schema.Types[t.NamedType]
	if def == nil {
		return nil, fmt.Errorf("unknown type %s at %s", t.NamedType, ctx)
	}
	if len(sel) == 0 { // leaf
		n, err := gqlType(schema, def, nil, ctx)
		if err != nil {
			return nil, err
		}
		if !t.NonNull {
			n["x-ct-nullable"] = true
		}
		return n, nil
	}
	props := node{}
	var required []string
	var walk func(sel ast.SelectionSet) error
	walk = func(sel ast.SelectionSet) error {
		for _, s := range sel {
			switch f := s.(type) {
			case *ast.Field:
				if f.Name == "__typename" {
					continue
				}
				fn, err := projectSelection(schema, f.Definition.Type, f.SelectionSet, ctx+"."+f.Name)
				if err != nil {
					return err
				}
				props[f.Alias] = fn
				required = append(required, f.Alias)
			case *ast.FragmentSpread:
				if err := walk(f.Definition.SelectionSet); err != nil {
					return err
				}
			case *ast.InlineFragment:
				// Conditional selections are optional by nature in v1.
				return fmt.Errorf("inline fragments at %s — type-conditional usage is not projectable in IR v1; select common fields or split operations", ctx)
			}
		}
		return nil
	}
	if err := walk(sel); err != nil {
		return nil, err
	}
	n := node{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		n["required"] = required
	}
	if !t.NonNull {
		n["x-ct-nullable"] = true
	}
	return n, nil
}
