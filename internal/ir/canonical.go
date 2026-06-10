package ir

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Canonicalize produces the canonical byte form of any JSON document:
// compact, object keys sorted, number literals preserved verbatim.
//
// Note: full RFC 8785 number re-formatting is deliberately deferred — IR
// documents are produced by our own normalizer, so literals are already
// stable across runs (NF3). Revisit if third-party extractors emit
// non-normalized number literals.
func Canonicalize(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("canonicalize: trailing data after JSON document")
	}
	// encoding/json marshals map keys in sorted order and json.Number verbatim.
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return out, nil
}

// CanonicalizeSchema marshals a Schema to canonical bytes. The schema must
// already be Normalized.
func CanonicalizeSchema(s *Schema) ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return Canonicalize(b)
}

// Hash returns the content address of a JSON document: "sha256:<hex>" over
// its canonical form.
func Hash(raw []byte) (string, error) {
	c, err := Canonicalize(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// HashSchema returns the content address of a normalized Schema.
func HashSchema(s *Schema) (string, error) {
	c, err := CanonicalizeSchema(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
