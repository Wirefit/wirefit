package yamlx

import (
	"strings"
	"testing"
)

type doc struct {
	Name string `yaml:"name"`
}

func TestDecodesSingleDocument(t *testing.T) {
	var d doc
	if err := StrictUnmarshal([]byte("name: a\n"), &d); err != nil {
		t.Fatal(err)
	}
	if d.Name != "a" {
		t.Errorf("name = %q, want a", d.Name)
	}
}

func TestExplicitStartMarkerIsStillOneDocument(t *testing.T) {
	var d doc
	if err := StrictUnmarshal([]byte("---\nname: a\n"), &d); err != nil {
		t.Fatal(err)
	}
	if d.Name != "a" {
		t.Errorf("name = %q, want a", d.Name)
	}
}

func TestRejectsUnknownField(t *testing.T) {
	var d doc
	err := StrictUnmarshal([]byte("name: a\nnaem: b\n"), &d)
	if err == nil {
		t.Fatal("typo'd key must error")
	}
	if !strings.Contains(err.Error(), "naem") {
		t.Errorf("error should name the unknown key: %v", err)
	}
}

// The whole point of this package: a bare Decode reads the first document and
// silently drops the rest, so half a config file would vanish without a word.
func TestRejectsSecondDocument(t *testing.T) {
	var d doc
	err := StrictUnmarshal([]byte("name: a\n---\nname: b\n"), &d)
	if err == nil {
		t.Fatal("second document must error, not be silently ignored")
	}
	if !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRejectsSecondDocumentWithUnknownFields(t *testing.T) {
	var d doc
	if err := StrictUnmarshal([]byte("name: a\n---\nwhatever: b\n"), &d); err == nil {
		t.Fatal("second document must error whatever it contains")
	}
}
