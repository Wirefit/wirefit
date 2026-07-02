package importer

// Avro importer (PRD 5.2): .avsc → IR.
//
// Mapping: records → objects (every field required — Avro always encodes all
// fields); ["null", T] unions → required + nullable; logical types → canonical
// scalars; enums → string enums; fixed → bytes; named-type reuse → recursion
// markers. Non-null unions are rejected: Avro's JSON encoding wraps them in
// type tags that no plain-JSON consumer parses — not representable (fail loud).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hamba/avro/v2"
)

func importAvro(projectDir, file, sel string) (json.RawMessage, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, file))
	if err != nil {
		return nil, err
	}
	schema, err := avro.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	target := schema
	if sel != "" {
		target = findNamed(schema, sel)
		if target == nil {
			return nil, fmt.Errorf("%s: named type %q not found", file, sel)
		}
	}
	n, err := avroNode(target, nil, file+"#"+sel)
	if err != nil {
		return nil, err
	}
	return marshal(n)
}

func findNamed(s avro.Schema, name string) avro.Schema {
	switch t := s.(type) {
	case *avro.RecordSchema:
		if t.Name() == name || t.FullName() == name {
			return t
		}
		for _, f := range t.Fields() {
			if found := findNamed(f.Type(), name); found != nil {
				return found
			}
		}
	case *avro.ArraySchema:
		return findNamed(t.Items(), name)
	case *avro.MapSchema:
		return findNamed(t.Values(), name)
	case *avro.UnionSchema:
		for _, m := range t.Types() {
			if found := findNamed(m, name); found != nil {
				return found
			}
		}
	case *avro.EnumSchema:
		if t.Name() == name || t.FullName() == name {
			return t
		}
	}
	return nil
}

func avroNode(s avro.Schema, stack []string, ctx string) (node, error) {
	switch t := s.(type) {
	case *avro.RecordSchema:
		for _, seen := range stack {
			if seen == t.FullName() {
				return node{"x-ct-recursive": true}, nil
			}
		}
		stack = append(stack, t.FullName())
		props := node{}
		var required []string
		for _, f := range t.Fields() {
			fn, err := avroNode(f.Type(), stack, ctx+"."+f.Name())
			if err != nil {
				return nil, err
			}
			props[f.Name()] = fn
			required = append(required, f.Name()) // Avro encodes every field
		}
		if len(props) == 0 {
			return nil, fmt.Errorf("record with no fields at %s", ctx)
		}
		sort.Strings(required)
		return node{"type": "object", "properties": props, "required": required}, nil

	case *avro.UnionSchema:
		types := t.Types()
		var nonNull []avro.Schema
		sawNull := false
		for _, m := range types {
			if m.Type() == avro.Null {
				sawNull = true
			} else {
				nonNull = append(nonNull, m)
			}
		}
		if !sawNull || len(nonNull) != 1 {
			return nil, fmt.Errorf("non-null avro union at %s: Avro's JSON union encoding is not plain-JSON compatible (IR v1)", ctx)
		}
		n, err := avroNode(nonNull[0], stack, ctx)
		if err != nil {
			return nil, err
		}
		n["x-ct-nullable"] = true
		return n, nil

	case *avro.EnumSchema:
		syms := append([]string(nil), t.Symbols()...)
		sort.Strings(syms)
		n := scalarNode("string")
		n["enum"] = syms
		return n, nil

	case *avro.ArraySchema:
		inner, err := avroNode(t.Items(), stack, ctx+"[]")
		if err != nil {
			return nil, err
		}
		return node{"type": "array", "items": inner}, nil

	case *avro.MapSchema:
		val, err := avroNode(t.Values(), stack, ctx+"{}")
		if err != nil {
			return nil, err
		}
		return node{"type": "object", "additionalProperties": val}, nil

	case *avro.FixedSchema:
		if lt := t.Logical(); lt != nil && lt.Type() == avro.Decimal {
			return scalarNode("decimal"), nil
		}
		return scalarNode("bytes"), nil

	case *avro.PrimitiveSchema:
		if lt := t.Logical(); lt != nil {
			switch lt.Type() {
			case avro.Decimal:
				return scalarNode("decimal"), nil
			case avro.UUID:
				return scalarNode("uuid"), nil
			case avro.Date:
				return scalarNode("date"), nil
			case avro.TimestampMillis, avro.TimestampMicros:
				return scalarNode("datetime"), nil
			}
		}
		switch t.Type() {
		case avro.String:
			return scalarNode("string"), nil
		case avro.Boolean:
			return scalarNode("bool"), nil
		case avro.Int:
			return scalarNode("int32"), nil
		case avro.Long:
			return scalarNode("int64"), nil
		case avro.Float:
			return scalarNode("float32"), nil
		case avro.Double:
			return scalarNode("float64"), nil
		case avro.Bytes:
			return scalarNode("bytes"), nil
		}
	case *avro.RefSchema:
		return avroNode(t.Schema(), stack, ctx)
	}
	return nil, fmt.Errorf("unsupported avro type %T at %s", s, ctx)
}
