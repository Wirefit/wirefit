package confexpected

import (
	"testing"

	"github.com/wirefit/wirefit/internal/ir"
)

// The committed corpus is the cross-language source of truth (every extractor
// must reproduce it hash-for-hash), so it is also the arbiter of how strict
// the IR validator may be. If a validation rule ever rejects a document here,
// the rule is wrong — not the fixture.
func TestCorpusIsValidIR(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("corpus is empty: the embed pattern stopped matching")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ir.Parse(raw); err != nil {
				t.Fatalf("corpus document fails IR validation: %v", err)
			}
		})
	}
}
