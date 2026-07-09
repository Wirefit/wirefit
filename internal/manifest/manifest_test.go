package manifest

import (
	"strings"
	"testing"
)

const valid = `
service: order-service
schema-version: 1
provides:
  - id: orders.get-order
    kind: rest
    direction: response
    dto: com.acme.orders.api.OrderResponse
  - id: orders.order-created
    kind: event
    direction: event
    dto: com.acme.orders.events.OrderCreated
consumes:
  - id: billing.invoice-created
    provider: billing-service
    dto: src/events/InvoiceCreated.ts
settings:
  unknown-fields: ignore
`

func TestValidManifest(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if m.RejectsUnknown() {
		t.Error("ignore must not report reject")
	}
}

func TestInvalidManifestReportsEveryProblem(t *testing.T) {
	bad := `
service: Order_Service
schema-version: 2
provides:
  - id: getorder
    kind: http
    direction: down
    dto: ""
  - id: orders.get-order
    kind: event
    direction: response
    dto: X
consumes:
  - id: orders.get-order
    provider: order-service
    dto: ""
settings:
  unknown-fields: explode
`
	m, err := Parse([]byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	errs := m.Validate()
	if len(errs) < 8 {
		t.Fatalf("expected ≥8 errors (one per problem), got %d: %v", len(errs), errs)
	}
}

func TestExtractorsValidation(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"suffix ok", `extractors: [{match: ".py", command: "wirefit-py"}]`, ""},
		{"wildcard ok", `extractors: [{match: "*", command: "wirefit-java"}]`, ""},
		{"bad match", `extractors: [{match: "py", command: "x"}]`, "file suffix"},
		{"missing command", `extractors: [{match: ".py"}]`, "command is required"},
		{"two wildcards", `extractors: [{match: "*", command: "a"}, {match: "*", command: "b"}]`, "only one"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte("service: x\nschema-version: 1\n" + c.yaml + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			errs := m.Validate()
			if c.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected validation errors: %v", errs)
				}
				return
			}
			for _, e := range errs {
				if strings.Contains(e.Error(), c.wantErr) {
					return
				}
			}
			t.Fatalf("no error containing %q in %v", c.wantErr, errs)
		})
	}
}

func TestConsumesFrom(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		provider, id string
		want         bool
	}{
		{"billing-service", "billing.invoice-created", true},
		{"billing-service", "orders.get-order", false},
		{"order-service", "billing.invoice-created", false},
	}
	for _, c := range cases {
		if got := m.ConsumesFrom(c.provider, c.id); got != c.want {
			t.Errorf("ConsumesFrom(%s, %s) = %v, want %v", c.provider, c.id, got, c.want)
		}
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	if _, err := Parse([]byte("service: x\nschema-version: 1\nproviides: []\n")); err == nil {
		t.Fatal("typo'd key must error (zero-config means typos fail loudly)")
	} else if !strings.Contains(err.Error(), "proviides") {
		t.Errorf("error should name the unknown key: %v", err)
	}
}
