// Package ir defines the unified intermediate representation (SPEC §7):
// a constrained JSON Schema subset with x-ct-* extensions, kept strictly
// canonical so identical logical schemas hash identically (NF3).
package ir

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Schema is one IR node. Absence ("not in Required") and nullability
// (Nullable) are deliberately distinct — see SPEC §7.
type Schema struct {
	Type               string             `json:"type,omitempty"`
	Scalar             Scalar             `json:"x-ct-scalar,omitempty"`
	Nullable           bool               `json:"x-ct-nullable,omitempty"`
	Recursive          bool               `json:"x-ct-recursive,omitempty"`
	Properties         map[string]*Schema `json:"properties,omitempty"`
	Required           []string           `json:"required,omitempty"`
	Items              *Schema            `json:"items,omitempty"`
	Enum               []string           `json:"enum,omitempty"`
	OneOf              []*Schema          `json:"oneOf,omitempty"`
	Discriminator      string             `json:"x-ct-discriminator,omitempty"`
	DiscriminatorValue string             `json:"x-ct-discriminator-value,omitempty"`
	// AdditionalProperties: nil = closed object; non-nil = open map.
	AdditionalProperties *AdditionalProps `json:"additionalProperties,omitempty"`
}

// AdditionalProps is an open map's additionalProperties. It marshals as either
// `true` (value type unexpressed) or a schema (the typed map value). Resolving
// SPEC open-question-2, every extractor now emits the value schema; bare `true`
// survives only for legacy / genuinely untyped maps.
type AdditionalProps struct {
	// Value is the map value schema, or nil when the value type is unexpressed.
	Value *Schema
	// closed records an explicit additionalProperties:false, which Normalize
	// collapses to the canonical closed form (a nil parent pointer).
	closed bool
}

func (ap AdditionalProps) MarshalJSON() ([]byte, error) {
	if ap.closed {
		return []byte("false"), nil
	}
	if ap.Value != nil {
		return json.Marshal(ap.Value)
	}
	return []byte("true"), nil
}

func (ap *AdditionalProps) UnmarshalJSON(data []byte) error {
	switch strings.TrimSpace(string(data)) {
	case "true":
		ap.Value = nil
		return nil
	case "false":
		ap.closed = true
		return nil
	}
	// Decode the value schema strictly: a custom unmarshaler otherwise loses the
	// DisallowUnknownFields that Parse applies to the rest of the document.
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var s Schema
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("additionalProperties: %w", err)
	}
	ap.Value = &s
	return nil
}

// MapValue returns the open map's value schema, or nil if the object is closed
// or its value type is unexpressed.
func (s *Schema) MapValue() *Schema {
	if s == nil || s.AdditionalProperties == nil {
		return nil
	}
	return s.AdditionalProperties.Value
}

// JSONKind returns the structural kind used for type-changed detection:
// "union", "object", "array", or the JSON primitive type. Scalar refinement
// (int32 vs int64) is handled separately via Fits.
func (s *Schema) JSONKind() string {
	switch {
	case s == nil:
		return ""
	case len(s.OneOf) > 0:
		return "union"
	case s.Properties != nil || s.Type == "object":
		return "object"
	case s.Items != nil || s.Type == "array":
		return "array"
	case s.Scalar != "":
		return s.Scalar.JSONType()
	default:
		return s.Type
	}
}

func (s *Schema) IsRequired(name string) bool {
	return s != nil && slices.Contains(s.Required, name)
}

// Normalize sorts every order-insensitive collection in place so that
// marshalling is deterministic. Must be called after deserialization.
func (s *Schema) Normalize() {
	if s == nil {
		return
	}
	slices.Sort(s.Required)
	s.Required = slices.Compact(s.Required)
	slices.Sort(s.Enum)
	s.Enum = slices.Compact(s.Enum)
	for _, c := range s.Properties {
		c.Normalize()
	}
	s.Items.Normalize()
	for _, b := range s.OneOf {
		b.Normalize()
	}
	slices.SortFunc(s.OneOf, func(a, b *Schema) int {
		return strings.Compare(a.DiscriminatorValue, b.DiscriminatorValue)
	})
	if s.AdditionalProperties != nil {
		if s.AdditionalProperties.closed {
			s.AdditionalProperties = nil
		} else {
			s.AdditionalProperties.Value.Normalize()
		}
	}
	if s.Scalar != "" && s.Type == "" {
		s.Type = s.Scalar.JSONType()
	}
}

// schemaKind mirrors JSONKind's precedence exactly, so the validator and the
// diff engine can never disagree about what a node is.
type schemaKind string

const (
	kindNone      schemaKind = ""
	kindRecursive schemaKind = "recursive"
	kindUnion     schemaKind = "union"
	kindObject    schemaKind = "object"
	kindArray     schemaKind = "array"
	kindScalar    schemaKind = "scalar"
)

func (s *Schema) kind() schemaKind {
	switch {
	case s == nil:
		return kindNone
	case s.Recursive:
		return kindRecursive
	case len(s.OneOf) > 0:
		return kindUnion
	case s.Properties != nil || s.Type == "object":
		return kindObject
	case s.Items != nil || s.Type == "array":
		return kindArray
	case s.Scalar != "" || s.Type != "":
		return kindScalar
	}
	return kindNone
}

// x-ct-nullable and x-ct-discriminator-value are orthogonal to shape, so they
// are allowed on any kind and deliberately absent from this table.
var kindFields = map[schemaKind]map[string]bool{
	kindRecursive: {"x-ct-recursive": true},
	kindUnion:     {"oneOf": true, "x-ct-discriminator": true},
	kindObject:    {"type": true, "properties": true, "required": true, "additionalProperties": true},
	kindArray:     {"type": true, "items": true},
	kindScalar:    {"type": true, "x-ct-scalar": true, "enum": true},
}

func (s *Schema) presentFields() []string {
	var out []string
	add := func(present bool, name string) {
		if present {
			out = append(out, name)
		}
	}
	add(s.Type != "", "type")
	add(s.Scalar != "", "x-ct-scalar")
	add(s.Recursive, "x-ct-recursive")
	add(s.Properties != nil, "properties")
	add(len(s.Required) > 0, "required")
	add(s.Items != nil, "items")
	add(len(s.Enum) > 0, "enum")
	add(len(s.OneOf) > 0, "oneOf")
	add(s.Discriminator != "", "x-ct-discriminator")
	add(s.AdditionalProperties != nil, "additionalProperties")
	return out
}

// Validate enforces the IR subset rules: every node commits to exactly one
// shape and carries only that shape's fields. Looseness here fails OPEN
// downstream — an unrecognized type compares equal to itself, an array without
// items matches any array, and a shapeless schema produces no findings against
// anything. Run it after Normalize.
func (s *Schema) Validate() error { return s.validate(false) }

// inProperty marks the one position where a node may legitimately have no
// shape — see the kindNone case.
func (s *Schema) validate(inProperty bool) error {
	if s == nil {
		return fmt.Errorf("nil schema")
	}
	if s.Scalar != "" && !s.Scalar.Valid() {
		return fmt.Errorf("unknown scalar %q", s.Scalar)
	}
	if s.Type != "" && !jsonTypes[s.Type] {
		return fmt.Errorf("unknown type %q", s.Type)
	}
	if s.Scalar != "" && s.Type != "" && s.Type != s.Scalar.JSONType() {
		return fmt.Errorf("scalar %q inconsistent with type %q", s.Scalar, s.Type)
	}

	k := s.kind()
	if k == kindNone {
		if f := s.presentFields(); len(f) > 0 {
			return fmt.Errorf("schema has no shape but carries %q", f[0])
		}
		// A shapeless property is a consumer projection: "this consumer reads
		// this field", asserting nothing about its type. Anywhere else it
		// would simply match everything.
		if inProperty {
			return nil
		}
		return fmt.Errorf("schema has no shape: expected one of type, x-ct-scalar, oneOf or x-ct-recursive")
	}
	for _, f := range s.presentFields() {
		if !kindFields[k][f] {
			return fmt.Errorf("%s schema must not carry %q", k, f)
		}
	}
	switch k {
	case kindObject:
		// No properties requirement: a bare {"type":"object"} is a closed
		// object with none, which is what an empty struct extracts to.
		if s.Type != "" && s.Type != "object" {
			return fmt.Errorf("object schema declares type %q", s.Type)
		}
	case kindArray:
		if s.Type != "" && s.Type != "array" {
			return fmt.Errorf("array schema declares type %q", s.Type)
		}
		if s.Items == nil {
			return fmt.Errorf("array schema requires items")
		}
	case kindScalar:
		if s.Scalar == "" {
			return fmt.Errorf("type %q requires x-ct-scalar: the JSON type alone does not pin the wire contract", s.Type)
		}
	case kindUnion:
		if s.Discriminator == "" {
			return fmt.Errorf("oneOf requires x-ct-discriminator (tagged unions only in v1)")
		}
	}

	for name, c := range s.Properties {
		if name == "" {
			return fmt.Errorf("empty property name")
		}
		if err := c.validate(true); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	for _, r := range s.Required {
		if s.Properties == nil || s.Properties[r] == nil {
			return fmt.Errorf("required field %q not in properties", r)
		}
	}
	if s.Items != nil {
		if err := s.Items.validate(false); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	if v := s.MapValue(); v != nil {
		if err := v.validate(false); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
	}
	if k == kindUnion {
		seen := map[string]bool{}
		for _, b := range s.OneOf {
			if b == nil {
				return fmt.Errorf("null oneOf branch")
			}
			if b.DiscriminatorValue == "" {
				return fmt.Errorf("oneOf branch missing x-ct-discriminator-value")
			}
			if seen[b.DiscriminatorValue] {
				return fmt.Errorf("duplicate oneOf branch %q", b.DiscriminatorValue)
			}
			seen[b.DiscriminatorValue] = true
			if err := b.validate(false); err != nil {
				return fmt.Errorf("oneOf %q: %w", b.DiscriminatorValue, err)
			}
			if b.kind() != kindObject {
				return fmt.Errorf("oneOf %q: branch must be an object", b.DiscriminatorValue)
			}
		}
	}
	return nil
}

// Load reads, parses, normalizes and validates an IR file.
func Load(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse parses, normalizes and validates IR JSON.
func Parse(data []byte) (*Schema, error) {
	var s Schema
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("invalid IR: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("invalid IR: trailing data after JSON document")
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid IR: %w", err)
	}
	return &s, nil
}
