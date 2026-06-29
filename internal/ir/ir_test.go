package ir

import (
	"strings"
	"testing"
)

func TestCanonicalizeIsOrderAndWhitespaceInsensitive(t *testing.T) {
	a := []byte(`{"b": 1, "a": {"y": true, "x": "v"}}`)
	b := []byte("{\n  \"a\": {\"x\": \"v\", \"y\": true},\n  \"b\": 1\n}")
	ca, err := Canonicalize(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Canonicalize(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("canonical forms differ:\n%s\n%s", ca, cb)
	}
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb || !strings.HasPrefix(ha, "sha256:") {
		t.Fatalf("hashes differ or malformed: %s vs %s", ha, hb)
	}
}

func TestCanonicalizePreservesNumberLiterals(t *testing.T) {
	c, err := Canonicalize([]byte(`{"n": 1.50, "m": 9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	s := string(c)
	if !strings.Contains(s, "1.50") {
		t.Errorf("decimal literal mangled: %s", s)
	}
	if !strings.Contains(s, "9007199254740993") {
		t.Errorf("int64 beyond 2^53 mangled (float round-trip?): %s", s)
	}
}

func TestParseNormalizeSortsAndHashesStable(t *testing.T) {
	s1, err := Parse([]byte(`{"type":"object","properties":{"a":{"x-ct-scalar":"string"},"b":{"x-ct-scalar":"int64"}},"required":["b","a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Parse([]byte(`{"type":"object","required":["a","b"],"properties":{"b":{"type":"integer","x-ct-scalar":"int64"},"a":{"type":"string","x-ct-scalar":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	h1, _ := HashSchema(s1)
	h2, _ := HashSchema(s2)
	if h1 != h2 {
		t.Fatalf("logically identical schemas hash differently: %s vs %s", h1, h2)
	}
}

func TestAdditionalPropertiesValueSchema(t *testing.T) {
	// Typed map value: the value schema round-trips and is reachable.
	s, err := Parse([]byte(`{"type":"object","additionalProperties":{"type":"string","x-ct-scalar":"string"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if mv := s.MapValue(); mv == nil || mv.Scalar != String {
		t.Fatalf("map value type not carried: %+v", s)
	}
	// map<string,string> and map<string,int32> must hash differently.
	hStr, _ := HashSchema(s)
	si, _ := Parse([]byte(`{"type":"object","additionalProperties":{"type":"integer","x-ct-scalar":"int32"}}`))
	hInt, _ := HashSchema(si)
	if hStr == hInt {
		t.Fatal("differently-typed maps hash identically — value type still discarded")
	}

	// Bare `true` survives as an untyped open map (nil value).
	open, err := Parse([]byte(`{"type":"object","additionalProperties":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if open.AdditionalProperties == nil || open.MapValue() != nil {
		t.Fatalf("untyped open map mishandled: %+v", open)
	}

	// `false` collapses to the canonical closed form (nil pointer).
	closed, err := Parse([]byte(`{"type":"object","properties":{"a":{"x-ct-scalar":"string"}},"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if closed.AdditionalProperties != nil {
		t.Fatalf("additionalProperties:false must collapse to closed: %+v", closed)
	}

	// A bad map value schema is rejected, and unknown keywords inside it are
	// rejected too — strictness is preserved through the custom unmarshaler.
	for _, bad := range []string{
		`{"type":"object","additionalProperties":{"x-ct-scalar":"long"}}`,
		`{"type":"object","additionalProperties":{"type":"string","bogus":1}}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestParseRejectsUnknownKeywordsAndBadIR(t *testing.T) {
	cases := []string{
		`{"type":"object","format":"weird"}`,                     // unknown keyword
		`{"x-ct-scalar":"long"}`,                                 // unknown scalar
		`{"type":"object","required":["ghost"]}`,                 // required not in properties
		`{"oneOf":[{"type":"object"}]}`,                          // union without discriminator
		`{"x-ct-discriminator":"t","oneOf":[{"type":"object"}]}`, // branch without value
		`{"type":"string","x-ct-scalar":"int64"}`,                // scalar/type mismatch
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c)); err == nil {
			t.Errorf("expected error for %s", c)
		}
	}
}

func TestFitsTable(t *testing.T) {
	cases := []struct {
		from, to Scalar
		want     Fit
	}{
		{Int32, Int64, FitOK},
		{Int64, Int32, FitNo},
		{Int64, Float64, FitLossy}, // the Java long → JS number case (SPEC F7)
		{Int32, Float64, FitOK},
		{Decimal, Float64, FitLossy},
		{Int64, Decimal, FitOK},
		{UUID, String, FitOK},
		{String, UUID, FitNo},
		{Bytes, UUID, FitNo},
		{Bool, Int32, FitNo},
		{Float64, Float32, FitLossy},
		{Float32, Float64, FitOK},
	}
	for _, c := range cases {
		if got := Fits(c.from, c.to); got != c.want {
			t.Errorf("Fits(%s,%s) = %d, want %d", c.from, c.to, got, c.want)
		}
	}
}
