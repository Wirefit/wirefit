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
	// AdditionalProperties: nil = unspecified (treated closed), true = open map.
	AdditionalProperties *bool `json:"additionalProperties,omitempty"`
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
	if s.Scalar != "" && s.Type == "" {
		s.Type = s.Scalar.JSONType()
	}
}

// Validate enforces the IR subset rules.
func (s *Schema) Validate() error {
	if s == nil {
		return fmt.Errorf("nil schema")
	}
	if s.Scalar != "" && !s.Scalar.Valid() {
		return fmt.Errorf("unknown scalar %q", s.Scalar)
	}
	if s.Scalar != "" && s.Type != "" && s.Type != s.Scalar.JSONType() {
		return fmt.Errorf("scalar %q inconsistent with type %q", s.Scalar, s.Type)
	}
	for name, c := range s.Properties {
		if name == "" {
			return fmt.Errorf("empty property name")
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	for _, r := range s.Required {
		if s.Properties == nil || s.Properties[r] == nil {
			return fmt.Errorf("required field %q not in properties", r)
		}
	}
	if s.Items != nil {
		if err := s.Items.Validate(); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	if len(s.OneOf) > 0 {
		if s.Discriminator == "" {
			return fmt.Errorf("oneOf requires x-ct-discriminator (tagged unions only in v1)")
		}
		seen := map[string]bool{}
		for _, b := range s.OneOf {
			if b.DiscriminatorValue == "" {
				return fmt.Errorf("oneOf branch missing x-ct-discriminator-value")
			}
			if seen[b.DiscriminatorValue] {
				return fmt.Errorf("duplicate oneOf branch %q", b.DiscriminatorValue)
			}
			seen[b.DiscriminatorValue] = true
			if err := b.Validate(); err != nil {
				return fmt.Errorf("oneOf %q: %w", b.DiscriminatorValue, err)
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
	s.Normalize()
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid IR: %w", err)
	}
	return &s, nil
}
