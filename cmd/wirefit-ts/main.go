// wirefit-ts: the official TypeScript extractor, an external executable
// speaking the public protocol (docs/extractor-protocol.md). Route to it per
// manifest: extractors: [{match: ".ts", command: "wirefit-ts"}].
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/tstool"
)

func main() { os.Exit(extserve.Serve(extract)) }

// Roles matter here: Zod `.default()` fields are required on the provider
// (output) side but optional on the consumer (input) side (PRD 2.3), so one
// schema cannot serve both.
func extract(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
	var provided, consumed []string
	roles := map[string]string{}
	for _, s := range specs {
		if r, ok := roles[s.Ref]; ok && r != s.Role {
			return nil, fmt.Errorf("%s is used in both provides and consumes; split the schema (zod io semantics differ per side)", s.Ref)
		}
		roles[s.Ref] = s.Role
		if s.Role == "provided" {
			provided = append(provided, s.Ref)
		} else {
			consumed = append(consumed, s.Ref)
		}
	}
	return tstool.Run(projectDir, provided, consumed)
}
