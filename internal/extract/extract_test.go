package extract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fake matches refs by suffix and records the spec groups it was handed.
type fake struct {
	suffix string
	calls  *[][]Spec
}

func (f fake) Match(ref string) bool { return f.suffix == "" || strings.HasSuffix(ref, f.suffix) }

func (f fake) Extract(projectDir string, specs []Spec) (map[string]json.RawMessage, error) {
	*f.calls = append(*f.calls, specs)
	out := map[string]json.RawMessage{}
	for _, s := range specs {
		out[s.Ref] = json.RawMessage(fmt.Sprintf("%q", f.suffix))
	}
	return out, nil
}

func TestRunRouting(t *testing.T) {
	tests := []struct {
		name     string
		suffixes []string // registry order; "" is a fallback
		specs    []Spec
		want     map[string][]Spec // suffix → expected sorted group
		wantErr  string
	}{
		{
			name:     "first match wins",
			suffixes: []string{".py", ".ts", ""},
			specs: []Spec{
				{Ref: "b.ts", Role: "provided"},
				{Ref: "a.py", Role: "consumed"},
				{Ref: "com.acme.Order", Role: "provided"},
			},
			want: map[string][]Spec{
				".py": {{Ref: "a.py", Role: "consumed"}},
				".ts": {{Ref: "b.ts", Role: "provided"}},
				"":    {{Ref: "com.acme.Order", Role: "provided"}},
			},
		},
		{
			name:     "groups sorted by ref then role",
			suffixes: []string{".ts"},
			specs: []Spec{
				{Ref: "z.ts", Role: "provided"},
				{Ref: "a.ts", Role: "provided"},
				{Ref: "a.ts", Role: "consumed"},
			},
			want: map[string][]Spec{
				".ts": {
					{Ref: "a.ts", Role: "consumed"},
					{Ref: "a.ts", Role: "provided"},
					{Ref: "z.ts", Role: "provided"},
				},
			},
		},
		{
			name:     "duplicate specs collapse",
			suffixes: []string{".ts"},
			specs: []Spec{
				{Ref: "a.ts", Role: "provided"},
				{Ref: "a.ts", Role: "provided"},
			},
			want: map[string][]Spec{
				".ts": {{Ref: "a.ts", Role: "provided"}},
			},
		},
		{
			name:     "no match fails with a suffix routing hint",
			suffixes: []string{".ts"},
			specs:    []Spec{{Ref: "a.py#Order", Role: "provided"}},
			wantErr:  `no extractor matches a.py#Order; add an extractors entry to contracts.yaml, e.g. {match: ".py", command: "<your-extractor>"}`,
		},
		{
			name:     "no match on a bare FQN hints the wildcard fallback",
			suffixes: []string{".ts"},
			specs:    []Spec{{Ref: "com.acme.Order", Role: "provided"}},
			wantErr:  `no extractor matches com.acme.Order; add an extractors entry to contracts.yaml, e.g. {match: "*", command: "wirefit-java"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := map[string]*[][]Spec{}
			var reg []Extractor
			for _, suf := range tt.suffixes {
				calls[suf] = &[][]Spec{}
				reg = append(reg, fake{suffix: suf, calls: calls[suf]})
			}
			out, err := Run(reg, ".", tt.specs)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for suf, want := range tt.want {
				got := *calls[suf]
				if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
					t.Errorf("%q extractor got %v, want one call with %v", suf, got, want)
				}
			}
			for _, s := range tt.specs {
				if _, ok := out[s.Ref]; !ok {
					t.Errorf("merged output missing %s", s.Ref)
				}
			}
		})
	}
}

func TestMatchSuffix(t *testing.T) {
	tests := []struct {
		ref  string
		sufs []string
		want bool
	}{
		{"api.ts#Order", []string{".ts", ".tsx"}, true},
		{"api.tsx#Order", []string{".ts", ".tsx"}, true},
		{"api.ts", []string{".ts"}, true},                // no selector: the whole ref is the file
		{"order.proto#Order.ts", []string{".ts"}, false}, // suffix matches the file, never the selector
		{"com.acme.Order", []string{".ts"}, false},
	}
	for _, tt := range tests {
		if got := MatchSuffix(tt.ref, tt.sufs...); got != tt.want {
			t.Errorf("MatchSuffix(%q, %v) = %v, want %v", tt.ref, tt.sufs, got, tt.want)
		}
	}
}

func TestExternalLetsExtractorHandleBothRoles(t *testing.T) {
	_, err := External{Suffixes: []string{".ts"}, Command: []string{"printf", `{"schemaVersion":1,"schemas":{"api.ts#Order":{"type":"string"}}}`}}.
		Extract(".", []Spec{
			{Ref: "api.ts#Order", Role: "consumed"},
			{Ref: "api.ts#Order", Role: "provided"},
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestRefs(t *testing.T) {
	got := Refs([]Spec{
		{Ref: "b", Role: "provided"},
		{Ref: "a", Role: "consumed"},
		{Ref: "a", Role: "provided"},
	})
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Refs = %v, want %v", got, want)
	}
}
