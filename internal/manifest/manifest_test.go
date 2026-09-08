package manifest

import (
	"reflect"
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
		{"argv list ok", `extractors: [{match: ".py", command: ["wirefit-py", "--python", ".venv/bin/python"]}]`, ""},
		{"empty argv list", `extractors: [{match: ".py", command: []}]`, "command is required"},
		{"blank command", `extractors: [{match: ".py", command: "   "}]`, "command is required"},
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

// A string command is split on whitespace; the list form is the only one that
// can carry an argument containing spaces.
func TestExtractorCommandForms(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       Command
	}{
		{"string", `command: "wirefit-py --python .venv/bin/python"`, Command{"wirefit-py", "--python", ".venv/bin/python"}},
		{"list", `command: ["wirefit-java", "--classpath", "/opt/my libs/a.jar"]`, Command{"wirefit-java", "--classpath", "/opt/my libs/a.jar"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte("service: x\nschema-version: 1\nextractors: [{match: \".py\", " + c.yaml + "}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			if errs := m.Validate(); len(errs) != 0 {
				t.Fatalf("unexpected validation errors: %v", errs)
			}
			if !reflect.DeepEqual(m.Extractors[0].Command, c.want) {
				t.Errorf("command = %q, want %q", m.Extractors[0].Command, c.want)
			}
		})
	}
}

func TestExtractorCommandRejectsOtherShapes(t *testing.T) {
	_, err := Parse([]byte("service: x\nschema-version: 1\nextractors: [{match: \".py\", command: {bin: x}}]\n"))
	if err == nil {
		t.Fatal("a mapping command must be rejected")
	}
	if !strings.Contains(err.Error(), "string or a list") {
		t.Errorf("unexpected error: %v", err)
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

// A second document would be dropped in silence by a bare Decode, quietly
// discarding half of someone's contracts.yaml.
func TestSecondDocumentRejected(t *testing.T) {
	_, err := Parse([]byte(valid + "---\nservice: other-service\nschema-version: 1\n"))
	if err == nil {
		t.Fatal("second document must error, not be silently ignored")
	}
	if !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateServiceName(t *testing.T) {
	for _, name := range []string{"orders", "order-service", "service-2"} {
		if err := ValidateServiceName(name); err != nil {
			t.Errorf("ValidateServiceName(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "../orders", "orders/api", `orders\api`, "/tmp/orders", "Order_Service"} {
		if err := ValidateServiceName(name); err == nil {
			t.Errorf("ValidateServiceName(%q) unexpectedly succeeded", name)
		}
	}
}
