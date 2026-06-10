package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/ir"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func importIR(t *testing.T, dir, spec string, opts Options) *ir.Schema {
	t.Helper()
	raw, err := Import(dir, spec, opts)
	if err != nil {
		t.Fatalf("%s: %v", spec, err)
	}
	s, err := ir.Parse(raw)
	if err != nil {
		t.Fatalf("%s: invalid IR: %v\n%s", spec, err, raw)
	}
	return s
}

func TestProtoImporter(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "order.proto", `
syntax = "proto3";
package acme;
import "google/protobuf/timestamp.proto";

message Order {
  string order_id = 1;
  int64 total_cents = 2;
  Status status = 3;
  repeated Item items = 4;
  optional string coupon = 5;
  google.protobuf.Timestamp created_at = 6;
  map<string, string> attributes = 7;
}
message Item {
  string sku = 1;
  int32 qty = 2;
}
enum Status {
  STATUS_UNSPECIFIED = 0;
  ACTIVE = 1;
  BLOCKED = 2;
}
`)
	s := importIR(t, dir, "order.proto#Order", Options{})
	if s.Properties["orderId"] == nil || s.Properties["totalCents"].Scalar != ir.Int64 {
		t.Fatalf("proto3 JSON names / scalars wrong: %+v", s.Properties)
	}
	if !s.IsRequired("orderId") || s.IsRequired("coupon") || s.IsRequired("items") {
		t.Errorf("presence wrong: required=%v", s.Required)
	}
	if s.Properties["createdAt"].Scalar != ir.DateTime {
		t.Error("Timestamp must map to datetime")
	}
	if got := s.Properties["status"].Enum; len(got) != 3 || got[0] != "ACTIVE" {
		t.Errorf("enum wrong: %v", got)
	}
	if ap := s.Properties["attributes"].AdditionalProperties; ap == nil || !*ap {
		t.Error("map must be an open object")
	}
}

func TestAvroImporter(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "order.avsc", `{
  "type": "record", "name": "OrderCreated", "namespace": "acme",
  "fields": [
    {"name": "id", "type": {"type": "string", "logicalType": "uuid"}},
    {"name": "totalCents", "type": "long"},
    {"name": "coupon", "type": ["null", "string"], "default": null},
    {"name": "status", "type": {"type": "enum", "name": "Status", "symbols": ["ACTIVE", "BLOCKED"]}},
    {"name": "items", "type": {"type": "array", "items": {
      "type": "record", "name": "Item", "fields": [
        {"name": "sku", "type": "string"},
        {"name": "qty", "type": "int"}
      ]}}},
    {"name": "createdAt", "type": {"type": "long", "logicalType": "timestamp-millis"}}
  ]
}`)
	s := importIR(t, dir, "order.avsc", Options{})
	if s.Properties["id"].Scalar != ir.UUID || s.Properties["createdAt"].Scalar != ir.DateTime {
		t.Errorf("logical types wrong: %+v", s.Properties)
	}
	if !s.Properties["coupon"].Nullable || !s.IsRequired("coupon") {
		t.Error("null-union must be required + nullable (Avro always encodes)")
	}
	if len(s.Required) != 6 {
		t.Errorf("all avro fields are required, got %v", s.Required)
	}
}

func TestGraphQLTypeAndOperation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "schema.graphql", `
type Query { order(id: ID!): Order }
type Order {
  id: ID!
  totalCents: Int!
  coupon: String
  status: Status!
  items: [Item!]!
}
type Item { sku: String! qty: Int! }
enum Status { ACTIVE BLOCKED }
`)
	s := importIR(t, dir, "schema.graphql#Order", Options{})
	if !s.IsRequired("coupon") || !s.Properties["coupon"].Nullable {
		t.Error("output nullable field: required + nullable")
	}
	if s.Properties["id"].Nullable {
		t.Error("ID! must be non-nullable")
	}

	// Persisted query: exact usage projection (PRD 5.4).
	write(t, dir, "getOrder.graphql", `
query GetOrder($id: ID!) {
  order(id: $id) { id totalCents }
}
`)
	p := importIR(t, dir, "getOrder.graphql", Options{GraphQLSchema: "schema.graphql"})
	if len(p.Properties) != 2 || p.Properties["coupon"] != nil {
		t.Errorf("projection must contain exactly the selected fields: %v", keys(p.Properties))
	}
	if !p.Nullable {
		t.Error("Order (nullable in SDL) projection must be nullable")
	}
}

func TestImporterErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bad.avsc", `{"type":"record","name":"X","fields":[
		{"name":"u","type":["string","long"]}]}`)
	if _, err := Import(dir, "bad.avsc", Options{}); err == nil || !strings.Contains(err.Error(), "union") {
		t.Errorf("non-null avro union must fail loudly: %v", err)
	}
	write(t, dir, "u64.proto", "syntax = \"proto3\";\nmessage M { uint64 n = 1; }\n")
	if _, err := Import(dir, "u64.proto#M", Options{}); err == nil || !strings.Contains(err.Error(), "uint64") {
		t.Errorf("uint64 must fail loudly: %v", err)
	}
}

func keys(m map[string]*ir.Schema) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

var _ = json.RawMessage{}
