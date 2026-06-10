package importer

// Proto importer (PRD 5.1, proto3 JSON mapping semantics).
//
// Deviations recorded in the PRD:
//   - oneof members map to optional fields, NOT IR oneOf: proto3 JSON has no
//     discriminator property — exclusivity is unexpressable in wire shape.
//   - field-number reuse detection deferred: the IR carries no field numbers
//     (they don't affect JSON wire compatibility); revisit with an IR
//     metadata channel if binary-proto checking is ever in scope.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func importProto(projectDir, file, message string) (json.RawMessage, error) {
	if message == "" {
		return nil, fmt.Errorf("%s: proto specs need a selector: %s#MessageName", file, file)
	}
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{projectDir},
		}),
	}
	files, err := compiler.Compile(context.Background(), file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	fd := files[0]
	if fd.Syntax() != protoreflect.Proto3 {
		return nil, fmt.Errorf("%s: only proto3 is supported (PRD 5.1; proto2 is P1)", file)
	}
	md := findMessage(fd.Messages(), message)
	if md == nil {
		return nil, fmt.Errorf("%s: message %q not found", file, message)
	}
	n, err := protoMessage(md, nil, file+"#"+message)
	if err != nil {
		return nil, err
	}
	return marshal(n)
}

func findMessage(msgs protoreflect.MessageDescriptors, name string) protoreflect.MessageDescriptor {
	for i := 0; i < msgs.Len(); i++ {
		m := msgs.Get(i)
		if string(m.Name()) == name {
			return m
		}
		if nested := findMessage(m.Messages(), name); nested != nil {
			return nested
		}
	}
	return nil
}

// Well-known types with dedicated JSON mappings.
var wellKnown = map[string]string{
	"google.protobuf.Timestamp": "datetime",
	"google.protobuf.Duration":  "duration",
}

// Wrapper types: nullable scalars in proto3 JSON.
var wrappers = map[string]string{
	"google.protobuf.StringValue": "string", "google.protobuf.BoolValue": "bool",
	"google.protobuf.Int32Value": "int32", "google.protobuf.Int64Value": "int64",
	"google.protobuf.UInt32Value": "int64", "google.protobuf.FloatValue": "float32",
	"google.protobuf.DoubleValue": "float64", "google.protobuf.BytesValue": "bytes",
}

func protoMessage(md protoreflect.MessageDescriptor, stack []protoreflect.FullName, ctx string) (node, error) {
	for _, s := range stack {
		if s == md.FullName() {
			return node{"x-ct-recursive": true}, nil
		}
	}
	stack = append(stack, md.FullName())

	props := node{}
	var required []string
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		name := f.JSONName() // proto3 JSON uses lowerCamel names
		fn, optional, err := protoField(f, stack, ctx+"."+name)
		if err != nil {
			return nil, err
		}
		props[name] = fn
		if !optional {
			required = append(required, name)
		}
	}
	if len(props) == 0 {
		return nil, fmt.Errorf("message with no fields at %s", ctx)
	}
	out := node{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out, nil
}

func protoField(f protoreflect.FieldDescriptor, stack []protoreflect.FullName, ctx string) (node, bool, error) {
	// proto3 JSON: empty repeated/map and unset messages/optionals are omitted.
	if f.IsMap() {
		if f.MapKey().Kind() != protoreflect.StringKind {
			return nil, false, fmt.Errorf("non-string map key at %s", ctx)
		}
		return node{"type": "object", "additionalProperties": true}, true, nil
	}
	if f.IsList() {
		inner, _, err := protoSingular(f, stack, ctx+"[]")
		if err != nil {
			return nil, false, err
		}
		return node{"type": "array", "items": inner}, true, nil
	}
	n, nullable, err := protoSingular(f, stack, ctx)
	if err != nil {
		return nil, false, err
	}
	if nullable {
		n["x-ct-nullable"] = true
	}
	// Explicit presence (optional keyword, message fields, oneof members)
	// → the field may be absent from the JSON document.
	optional := f.HasPresence()
	return n, optional, nil
}

func protoSingular(f protoreflect.FieldDescriptor, stack []protoreflect.FullName, ctx string) (node, bool, error) {
	switch f.Kind() {
	case protoreflect.StringKind:
		return scalarNode("string"), false, nil
	case protoreflect.BoolKind:
		return scalarNode("bool"), false, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return scalarNode("int32"), false, nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return scalarNode("int64"), false, nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return nil, false, fmt.Errorf("uint64 at %s may exceed int64 — not contract-checkable (use int64 or string)", ctx)
	case protoreflect.FloatKind:
		return scalarNode("float32"), false, nil
	case protoreflect.DoubleKind:
		return scalarNode("float64"), false, nil
	case protoreflect.BytesKind:
		return scalarNode("bytes"), false, nil
	case protoreflect.EnumKind:
		vals := f.Enum().Values()
		var names []string
		for i := 0; i < vals.Len(); i++ {
			names = append(names, string(vals.Get(i).Name()))
		}
		sort.Strings(names)
		n := scalarNode("string")
		n["enum"] = names
		return n, false, nil
	case protoreflect.MessageKind:
		full := string(f.Message().FullName())
		if s, ok := wellKnown[full]; ok {
			return scalarNode(s), false, nil
		}
		if s, ok := wrappers[full]; ok {
			return scalarNode(s), true, nil // wrappers serialize as nullable scalars
		}
		n, err := protoMessage(f.Message(), stack, ctx)
		return n, false, err
	}
	return nil, false, fmt.Errorf("unsupported proto kind %s at %s", f.Kind(), ctx)
}
