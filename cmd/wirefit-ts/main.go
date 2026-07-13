// wirefit-ts: the official TypeScript extractor, an external executable
// speaking the public protocol (docs/extractor-protocol.md). Route to it per
// manifest: extractors: [{match: ".ts", command: "wirefit-ts"}].
package main

import (
	"encoding/json"
	"os"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/tstool"
)

func main() { os.Exit(extserve.Serve(extract)) }

var runTS = tstool.Run

// Roles matter here: Zod `.default()` fields are required on the provider
// (output) side but optional on the consumer (input) side (PRD 2.3), so one
// schema cannot serve both.
func extract(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
	provided, consumed, err := extserve.SplitRoles(specs, "zod io semantics differ per side")
	if err != nil {
		return nil, err
	}
	return runTS(projectDir, provided, consumed)
}
