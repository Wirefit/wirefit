package diff

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wirefit/wirefit/internal/ir"
)

func sch(t *testing.T, s string) *ir.Schema {
	t.Helper()
	p, err := ir.Parse([]byte(s))
	if err != nil {
		t.Fatalf("bad fixture schema: %v\n%s", err, s)
	}
	return p
}

const (
	objIDEmail    = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string"}},"required":["email","id"]}`
	objIDOnly     = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"}},"required":["id"]}`
	objIDEmailOpt = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string"}},"required":["id"]}`
	consumesEmail = `{"type":"object","properties":{"email":{"x-ct-scalar":"string"}},"required":["email"]}`
	consumesID    = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"}},"required":["id"]}`

	statusAB    = `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["ACTIVE","BLOCKED"]}},"required":["status"]}`
	statusABC   = `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["ACTIVE","BLOCKED","CLOSED"]}},"required":["status"]}`
	readsStatus = `{"type":"object","properties":{"status":{"x-ct-scalar":"string","enum":["ACTIVE","BLOCKED"]}},"required":["status"]}`

	countI32   = `{"type":"object","properties":{"count":{"x-ct-scalar":"int32"}},"required":["count"]}`
	countI64   = `{"type":"object","properties":{"count":{"x-ct-scalar":"int64"}},"required":["count"]}`
	countF64   = `{"type":"object","properties":{"count":{"x-ct-scalar":"float64"}},"required":["count"]}`
	readsCount = `{"type":"object","properties":{"count":{}}}`

	emailNullable = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string","x-ct-nullable":true}},"required":["email","id"]}`

	nestedReq   = `{"type":"object","properties":{"address":{"type":"object","properties":{"street":{"x-ct-scalar":"string"},"city":{"x-ct-scalar":"string"}},"required":["street"]}},"required":["address"]}`
	nestedOpt   = `{"type":"object","properties":{"address":{"type":"object","properties":{"street":{"x-ct-scalar":"string"},"city":{"x-ct-scalar":"string"}}}},"required":["address"]}`
	readsStreet = `{"type":"object","properties":{"address":{"type":"object","properties":{"street":{"x-ct-scalar":"string"}},"required":["street"]}},"required":["address"]}`

	petCatDog = `{"type":"object","properties":{"pet":{"x-ct-discriminator":"kind","oneOf":[
		{"type":"object","x-ct-discriminator-value":"cat","properties":{"lives":{"x-ct-scalar":"int32"}}},
		{"type":"object","x-ct-discriminator-value":"dog","properties":{"barks":{"x-ct-scalar":"bool"}}}]}},"required":["pet"]}`
	petCatDogBird = `{"type":"object","properties":{"pet":{"x-ct-discriminator":"kind","oneOf":[
		{"type":"object","x-ct-discriminator-value":"bird","properties":{"sings":{"x-ct-scalar":"bool"}}},
		{"type":"object","x-ct-discriminator-value":"cat","properties":{"lives":{"x-ct-scalar":"int32"}}},
		{"type":"object","x-ct-discriminator-value":"dog","properties":{"barks":{"x-ct-scalar":"bool"}}}]}},"required":["pet"]}`
	readsPet = `{"type":"object","properties":{"pet":{"x-ct-discriminator":"kind","oneOf":[
		{"type":"object","x-ct-discriminator-value":"cat","properties":{"lives":{"x-ct-scalar":"int32"}}},
		{"type":"object","x-ct-discriminator-value":"dog","properties":{"barks":{"x-ct-scalar":"bool"}}}]}},"required":["pet"]}`

	idAsInt      = `{"type":"object","properties":{"id":{"x-ct-scalar":"int32"},"email":{"x-ct-scalar":"string"}},"required":["email","id"]}`
	withPhone    = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string"},"phone":{"x-ct-scalar":"string"}},"required":["email","id"]}`
	withReqPhone = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string"},"phone":{"x-ct-scalar":"string"}},"required":["email","id","phone"]}`
	sendsIDEmail = `{"type":"object","properties":{"id":{"x-ct-scalar":"uuid"},"email":{"x-ct-scalar":"string"}},"required":["email","id"]}`
)

type selfCase struct {
	name           string
	dir            Direction
	before, after  string
	consumers      map[string]string // name → projection JSON
	strict         map[string]bool   // consumer → RejectUnknown
	providerReject bool
	coldStart      bool
	want           []string // "class rule path"
}

func TestSelfDiffRuleCorpus(t *testing.T) {
	cases := []selfCase{
		// ---- P→C: producer emits, consumers parse (responses, events) ----
		{name: "p2c field removed, consumed", dir: P2C, before: objIDEmail, after: objIDOnly,
			consumers: map[string]string{"web": consumesEmail},
			want:      []string{"breaking field-removed $.email"}},
		{name: "p2c field removed, unconsumed — the CDC payoff", dir: P2C, before: objIDEmail, after: objIDOnly,
			consumers: map[string]string{"web": consumesID},
			want:      []string{"safe field-removed $.email"}},
		{name: "p2c field removed, cold start downgrades to warning", dir: P2C, before: objIDEmail, after: objIDOnly,
			coldStart: true,
			want:      []string{"warning field-removed $.email"}},
		{name: "p2c required→optional is breaking", dir: P2C, before: objIDEmail, after: objIDEmailOpt,
			consumers: map[string]string{"web": consumesEmail},
			want:      []string{"breaking required-to-optional $.email"}},
		{name: "p2c optional→required is safe", dir: P2C, before: objIDEmailOpt, after: objIDEmail,
			consumers: map[string]string{"web": consumesEmail},
			want:      []string{"safe optional-to-required $.email"}},
		{name: "p2c field added is safe", dir: P2C, before: objIDEmail, after: withPhone,
			consumers: map[string]string{"web": consumesEmail},
			want:      []string{"safe field-added $.phone"}},
		{name: "p2c field added breaks strict consumers", dir: P2C, before: objIDEmail, after: withPhone,
			consumers: map[string]string{"web": consumesEmail}, strict: map[string]bool{"web": true},
			want: []string{"breaking field-added $.phone"}},
		{name: "p2c nullable added, consumed", dir: P2C, before: objIDEmail, after: emailNullable,
			consumers: map[string]string{"web": consumesEmail},
			want:      []string{"breaking nullable-added $.email"}},
		{name: "p2c nullable added, unconsumed", dir: P2C, before: objIDEmail, after: emailNullable,
			consumers: map[string]string{"web": consumesID},
			want:      []string{"safe nullable-added $.email"}},
		{name: "p2c enum value added is breaking", dir: P2C, before: statusAB, after: statusABC,
			consumers: map[string]string{"web": readsStatus},
			want:      []string{"breaking enum-value-added $.status"}},
		{name: "p2c enum value removed is safe", dir: P2C, before: statusABC, after: statusAB,
			consumers: map[string]string{"web": readsStatus},
			want:      []string{"safe enum-value-removed $.status"}},
		{name: "p2c scalar widening breaks consumers", dir: P2C, before: countI32, after: countI64,
			consumers: map[string]string{"web": readsCount},
			want:      []string{"breaking scalar-changed $.count"}},
		{name: "p2c scalar narrowing is safe", dir: P2C, before: countI64, after: countI32,
			consumers: map[string]string{"web": readsCount},
			want:      []string{"safe scalar-changed $.count"}},
		{name: "p2c float64→int64 is lossy warning", dir: P2C, before: countF64, after: countI64,
			consumers: map[string]string{"web": readsCount},
			want:      []string{"warning scalar-lossy $.count"}},
		{name: "p2c type kind change is breaking", dir: P2C, before: objIDEmail, after: idAsInt,
			consumers: map[string]string{"web": consumesID},
			want:      []string{"breaking type-changed $.id"}},
		{name: "p2c nested required shift cascades to leaf path", dir: P2C, before: nestedReq, after: nestedOpt,
			consumers: map[string]string{"web": readsStreet},
			want:      []string{"breaking required-to-optional $.address.street"}},
		{name: "p2c union branch added is breaking at union node", dir: P2C, before: petCatDog, after: petCatDogBird,
			consumers: map[string]string{"web": readsPet},
			want:      []string{"breaking union-branch-added $.pet"}},
		{name: "p2c union branch removed is safe", dir: P2C, before: petCatDogBird, after: petCatDog,
			consumers: map[string]string{"web": readsPet},
			want:      []string{"safe union-branch-removed $.pet<bird>"}},

		// ---- C→P: consumers emit, provider parses (requests, commands) ----
		{name: "c2p required field added breaks non-senders", dir: C2P, before: objIDEmail, after: withReqPhone,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"breaking required-field-added $.phone"}},
		{name: "c2p optional field added is safe", dir: C2P, before: objIDEmail, after: withPhone,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"safe field-added $.phone"}},
		{name: "c2p optional→required breaks consumers not sending it", dir: C2P, before: withPhone, after: withReqPhone,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"breaking optional-to-required $.phone"}},
		{name: "c2p optional→required safe when all consumers already send it", dir: C2P, before: objIDEmailOpt, after: objIDEmail,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"safe optional-to-required $.email"}},
		{name: "c2p required→optional is safe", dir: C2P, before: objIDEmail, after: objIDEmailOpt,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"safe required-to-optional $.email"}},
		{name: "c2p field removed is safe when unknown fields ignored", dir: C2P, before: objIDEmail, after: objIDOnly,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"safe field-removed $.email"}},
		{name: "c2p field removed breaks under strict provider", dir: C2P, before: objIDEmail, after: objIDOnly,
			consumers: map[string]string{"web": sendsIDEmail}, providerReject: true,
			want: []string{"breaking field-removed $.email"}},
		{name: "c2p enum value removed is breaking", dir: C2P, before: statusABC, after: statusAB,
			consumers: map[string]string{"web": readsStatus},
			want:      []string{"breaking enum-value-removed $.status"}},
		{name: "c2p enum value added is safe", dir: C2P, before: statusAB, after: statusABC,
			consumers: map[string]string{"web": readsStatus},
			want:      []string{"safe enum-value-added $.status"}},
		{name: "c2p scalar widening of accepted set is safe", dir: C2P, before: countI32, after: countI64,
			consumers: map[string]string{"web": readsCount},
			want:      []string{"safe scalar-changed $.count"}},
		{name: "c2p scalar narrowing of accepted set is breaking", dir: C2P, before: countI64, after: countI32,
			consumers: map[string]string{"web": readsCount},
			want:      []string{"breaking scalar-changed $.count"}},
		{name: "c2p nullable accepted then rejected is breaking", dir: C2P, before: emailNullable, after: objIDEmail,
			consumers: map[string]string{"web": sendsIDEmail},
			want:      []string{"breaking nullable-removed $.email"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := SelfOptions{
				Direction:              tc.dir,
				Consumers:              map[string]Consumer{},
				ProviderRejectsUnknown: tc.providerReject,
				ColdStart:              tc.coldStart,
			}
			for name, js := range tc.consumers {
				opts.Consumers[name] = Consumer{Schema: sch(t, js), RejectUnknown: tc.strict[name]}
			}
			r := Self(sch(t, tc.before), sch(t, tc.after), opts)

			var got []string
			for _, f := range r.Findings {
				got = append(got, fmt.Sprintf("%s %s %s", f.Class, f.Rule, f.Path))
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("findings mismatch\n got: %v\nwant: %v\nfull: %+v", got, want, r.Findings)
			}
			if tc.coldStart {
				if !r.ColdStart {
					t.Error("result not marked cold start")
				}
				if r.ExitCode() != 0 {
					t.Error("cold start must not block (exit 0)")
				}
			}
		})
	}
}

func TestSelfDiffDeterminism(t *testing.T) {
	opts := SelfOptions{Direction: P2C, Consumers: map[string]Consumer{
		"a": {Schema: sch(t, consumesEmail)}, "b": {Schema: sch(t, consumesEmail)},
	}}
	r1 := Self(sch(t, objIDEmail), sch(t, objIDOnly), opts)
	r2 := Self(sch(t, objIDEmail), sch(t, objIDOnly), opts)
	if fmt.Sprintf("%+v", r1) != fmt.Sprintf("%+v", r2) {
		t.Error("identical inputs produced different results (NF3)")
	}
	if len(r1.Findings) != 1 || !slices.Equal(r1.Findings[0].ConsumedBy, []string{"a", "b"}) {
		t.Errorf("consumed-by list wrong or unsorted: %+v", r1.Findings)
	}
}
