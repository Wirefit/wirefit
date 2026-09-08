package gotool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The extractor's promotion rules only matter insofar as they agree with
// encoding/json, so each fixture is checked two ways: the extracted IR's
// property names against the keys json.Marshal actually emits for a populated
// value, and the IR's own optional/type decisions against what the mapping
// promises.

type embedFixture struct {
	name  string
	src   string // body of package dto, declaring Order
	value string // populated dto.Order expression
}

const fixtureModule = "wirefit.test/fixture"

func writeFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+fixtureModule+"\n\ngo 1.25\n")
	write("dto/dto.go", "package dto\n\n"+src)
	return dir
}

func extractOrder(t *testing.T, dir string) map[string]any {
	t.Helper()
	out, err := Run(dir, []string{"./dto#Order"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	raw, ok := out["./dto#Order"]
	if !ok {
		t.Fatalf("no schema for ./dto#Order in %v", out)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func propNames(t *testing.T, schema map[string]any) []string {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", schema)
	}
	var names []string
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func requiredNames(schema map[string]any) []string {
	var names []string
	for _, v := range schema["required"].([]any) {
		names = append(names, v.(string))
	}
	sort.Strings(names)
	return names
}

func scalarOf(t *testing.T, schema map[string]any, prop string) string {
	t.Helper()
	p, ok := schema["properties"].(map[string]any)[prop].(map[string]any)
	if !ok {
		t.Fatalf("no property %q in %v", prop, schema)
	}
	s, _ := p["x-ct-scalar"].(string)
	return s
}

// marshalKeys reports the top-level JSON keys encoding/json emits for value,
// compiled inside the fixture module so it sees the same types the extractor did.
func marshalKeys(t *testing.T, dir, value string) []string {
	t.Helper()
	main := `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"` + fixtureModule + `/dto"
)

func main() {
	b, err := json.Marshal(` + value + `)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(strings.Join(keys, " "))
}
`
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keys", "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./keys")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("marshal helper: %v", err)
	}
	return strings.Fields(string(out))
}

var embedFixtures = []embedFixture{
	{
		name: "EmbeddedValue",
		src: `type Base struct {
	ID string ` + "`json:\"id\"`" + `
}

type Order struct {
	Base
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{Base: dto.Base{ID: "x"}, Total: 1}`,
	},
	{
		name: "EmbeddedPointer",
		src: `type Base struct {
	ID string ` + "`json:\"id\"`" + `
}

type Order struct {
	*Base
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{Base: &dto.Base{ID: "x"}, Total: 1}`,
	},
	{
		name: "TaggedAnonymous",
		src: `type Base struct {
	ID string ` + "`json:\"id\"`" + `
}

type Order struct {
	Base  ` + "`json:\"base\"`" + `
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{Base: dto.Base{ID: "x"}, Total: 1}`,
	},
	{
		name: "OuterFieldWinsOverPromoted",
		src: `type Base struct {
	ID string ` + "`json:\"id\"`" + `
}

type Order struct {
	Base
	ID    int64 ` + "`json:\"id\"`" + `
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{Base: dto.Base{ID: "x"}, ID: 7, Total: 1}`,
	},
	{
		name: "SameDepthTieBrokenByTag",
		src: `type A struct {
	Ref string ` + "`json:\"Ref\"`" + `
}

type B struct {
	Ref int64
}

type Order struct {
	A
	B
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{A: dto.A{Ref: "x"}, B: dto.B{Ref: 7}, Total: 1}`,
	},
	{
		name: "UnexportedEmbeddedStruct",
		src: `type base struct {
	ID string ` + "`json:\"id\"`" + `
}

type Order struct {
	base
	Total int64 ` + "`json:\"total\"`" + `
}`,
		value: `dto.Order{Total: 1}`,
	},
	{
		name: "Omitzero",
		src: `type Order struct {
	ID    string ` + "`json:\"id\"`" + `
	Total int64  ` + "`json:\"total,omitzero\"`" + `
}`,
		value: `dto.Order{ID: "x", Total: 1}`,
	},
}

func TestExtractedPropertiesMatchEncodingJSON(t *testing.T) {
	for _, f := range embedFixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			dir := writeFixture(t, f.src)
			got := propNames(t, extractOrder(t, dir))
			want := marshalKeys(t, dir, f.value)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("extracted properties %v, encoding/json emits %v", got, want)
			}
		})
	}
}

func TestEmbeddedPointerFieldsAreNotRequired(t *testing.T) {
	t.Parallel()
	// A nil *Base drops id from the payload, so a contract that required it
	// would describe a shape the service does not always produce.
	dir := writeFixture(t, `type Base struct {
	ID string `+"`json:\"id\"`"+`
}

type Order struct {
	*Base
	Total int64 `+"`json:\"total\"`"+`
}`)
	if got, want := requiredNames(extractOrder(t, dir)), []string{"total"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("required = %v, want %v", got, want)
	}
}

func TestOuterFieldWinsOverPromotedField(t *testing.T) {
	t.Parallel()
	dir := writeFixture(t, `type Base struct {
	ID string `+"`json:\"id\"`"+`
}

type Order struct {
	Base
	ID    int64 `+"`json:\"id\"`"+`
	Total int64 `+"`json:\"total\"`"+`
}`)
	if got := scalarOf(t, extractOrder(t, dir), "id"); got != "int64" {
		t.Errorf("id resolved to %q, want the shallower outer field (int64)", got)
	}
}

func TestSameDepthTieResolvesToTaggedField(t *testing.T) {
	t.Parallel()
	dir := writeFixture(t, `type A struct {
	Ref string `+"`json:\"Ref\"`"+`
}

type B struct {
	Ref int64
}

type Order struct {
	A
	B
	Total int64 `+"`json:\"total\"`"+`
}`)
	if got := scalarOf(t, extractOrder(t, dir), "Ref"); got != "string" {
		t.Errorf("Ref resolved to %q, want the tagged field (string)", got)
	}
}

func TestOmitzeroFieldIsOptional(t *testing.T) {
	t.Parallel()
	dir := writeFixture(t, `type Order struct {
	ID    string `+"`json:\"id\"`"+`
	Total int64  `+"`json:\"total,omitzero\"`"+`
}`)
	if got, want := requiredNames(extractOrder(t, dir)), []string{"id"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("required = %v, want %v", got, want)
	}
}

func TestUnresolvedPromotionConflictFails(t *testing.T) {
	t.Parallel()
	// encoding/json drops id here; a contract cannot silently lose a field.
	dir := writeFixture(t, `type A struct {
	ID string `+"`json:\"id\"`"+`
}

type B struct {
	ID string `+"`json:\"id\"`"+`
}

type Order struct {
	A
	B
	Total int64 `+"`json:\"total\"`"+`
}`)
	_, err := Run(dir, []string{"./dto#Order"})
	if err == nil {
		t.Fatal("expected an error for an unresolvable promotion conflict")
	}
}

func TestConcurrentRunsDoNotClobberEachOther(t *testing.T) {
	dir := writeFixture(t, `type Order struct {
	ID string `+"`json:\"id\"`"+`
}`)
	// A pre-existing sibling proves cleanup is scoped to this run's directory.
	keep := filepath.Join(dir, ".wirefit", "gen", "keep")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Run(dir, []string{"./dto#Order"})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent run %d: %v", i, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated .wirefit/gen content was removed: %v", err)
	}
}
