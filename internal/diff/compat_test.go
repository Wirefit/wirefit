package diff

import (
	"fmt"
	"slices"
	"testing"
)

type compatCase struct {
	name               string
	dir                Direction
	provider, consumer string
	strictParser       bool
	want               []string
}

func TestCompatCorpus(t *testing.T) {
	providerFull := `{"type":"object","properties":{
		"id":{"x-ct-scalar":"uuid"},
		"total":{"x-ct-scalar":"int64"},
		"note":{"x-ct-scalar":"string","x-ct-nullable":true},
		"status":{"x-ct-scalar":"string","enum":["ACTIVE","BLOCKED","CLOSED"]}},
		"required":["id","status","total"]}`

	cases := []compatCase{
		{name: "p2c consumer subset of provider is compatible", dir: P2C,
			provider: providerFull,
			consumer: `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"}},"required":["id"]}`,
			want:     nil},
		{name: "p2c consumer reads field provider lacks", dir: P2C,
			provider: providerFull,
			consumer: `{"type":"object","properties":{"email":{"x-ct-scalar":"string"}},"required":["email"]}`,
			want:     []string{"breaking field-missing $.email"}},
		{name: "p2c consumer requires what provider only may send", dir: P2C,
			provider: `{"type":"object","properties":{"note":{"x-ct-scalar":"string"}}}`,
			consumer: `{"type":"object","properties":{"note":{"x-ct-scalar":"string"}},"required":["note"]}`,
			want:     []string{"breaking presence-not-guaranteed $.note"}},
		{name: "p2c java long parsed as TS number is the canonical lossiness warning", dir: P2C,
			provider: providerFull,
			consumer: `{"type":"object","properties":{"total":{"x-ct-scalar":"float64"}},"required":["total"]}`,
			want:     []string{"warning scalar-lossy $.total"}},
		{name: "p2c provider enum exceeds consumer's known values", dir: P2C,
			provider: providerFull,
			consumer: `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["ACTIVE","BLOCKED"]}},"required":["status"]}`,
			want:     []string{"breaking enum-unknown-value $.status"}},
		{name: "p2c nullable emitted into non-nullable parser", dir: P2C,
			provider: providerFull,
			consumer: `{"type":"object","properties":{"note":{"x-ct-scalar":"string"}}}`,
			want:     []string{"breaking nullability-mismatch $.note"}},
		{name: "p2c strict consumer rejects unknown provider fields", dir: P2C,
			provider:     providerFull,
			consumer:     `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"}},"required":["id"]}`,
			strictParser: true,
			want: []string{
				"breaking unknown-field-rejected $.note",
				"breaking unknown-field-rejected $.status",
				"breaking unknown-field-rejected $.total",
			}},
		{name: "p2c provider untyped map values into a consumer expecting a fixed type", dir: P2C,
			provider: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":true}},"required":["attrs"]}`,
			consumer: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"string"}}},"required":["attrs"]}`,
			want:     []string{"breaking map-value-open-vs-typed $.attrs{}"}},
		{name: "p2c matching typed map values are compatible", dir: P2C,
			provider: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"string"}}},"required":["attrs"]}`,
			consumer: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"string"}}},"required":["attrs"]}`,
			want:     nil},
		{name: "c2p consumer omits provider-required request field", dir: C2P,
			provider: `{"type":"object","properties":{"qty":{"x-ct-scalar":"int32"},"sku":{"x-ct-scalar":"string"}},"required":["qty","sku"]}`,
			consumer: `{"type":"object","properties":{"sku":{"x-ct-scalar":"string"}},"required":["sku"]}`,
			want:     []string{"breaking field-missing $.qty"}},
		{name: "c2p consumer sends within provider's accepted enum", dir: C2P,
			provider: `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["A","B","C"]}},"required":["status"]}`,
			consumer: `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["A"]}},"required":["status"]}`,
			want:     nil},
		{name: "c2p consumer sends untyped map values the provider parses as a fixed type", dir: C2P,
			provider: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"string"}}},"required":["attrs"]}`,
			consumer: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":true}},"required":["attrs"]}`,
			want:     []string{"breaking map-value-open-vs-typed $.attrs{}"}},
		{name: "c2p consumer map values too wide for provider", dir: C2P,
			provider: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"int32"}}},"required":["attrs"]}`,
			consumer: `{"type":"object","properties":{"attrs":{"type":"object","additionalProperties":{"x-ct-scalar":"int64"}}},"required":["attrs"]}`,
			want:     []string{"breaking scalar-mismatch $.attrs{}"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compat(sch(t, tc.provider), sch(t, tc.consumer), CompatOptions{
				Direction: tc.dir, StrictParser: tc.strictParser,
			})
			var got []string
			for _, f := range r.Findings {
				got = append(got, fmt.Sprintf("%s %s %s", f.Class, f.Rule, f.Path))
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("findings mismatch\n got: %v\nwant: %v", got, want)
			}
		})
	}
}
