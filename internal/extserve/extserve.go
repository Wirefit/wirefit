// Package extserve implements the extractor side of the public protocol
// (docs/extractor-protocol.md): one Request on stdin, one Response on stdout.
// It is the shared main-loop of the official external extractor binaries
// (wirefit-ts, wirefit-java, wirefit-py); third parties are free to speak the protocol
// directly.
package extserve

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/wirefit/wirefit/internal/extproto"
)

// Serve reads a Request from stdin, dispatches to fn and writes the Response
// to stdout, returning the process exit code. Failures travel in the Response
// body with exit 0 (protocol convention, like the python reference
// implementation): the caller reads the error from the body, a nonzero exit
// is reserved for not being able to produce a Response at all.
func Serve(fn func(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error)) int {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return respond(nil, fmt.Errorf("reading request: %v", err))
	}
	var req extproto.Request
	if err := json.Unmarshal(in, &req); err != nil {
		return respond(nil, fmt.Errorf("invalid request JSON: %v", err))
	}
	if req.SchemaVersion != extproto.SchemaVersion {
		return respond(nil, fmt.Errorf("protocol version %d, want %d", req.SchemaVersion, extproto.SchemaVersion))
	}
	schemas, err := fn(req.ProjectDir, req.Specs)
	return respond(schemas, err)
}

// SplitRoles partitions specs by manifest role for a role-sensitive
// extractor and rejects a ref used on both sides; why names the serializer
// semantics that keep one schema from serving both, e.g. "zod io semantics
// differ per side".
func SplitRoles(specs []extproto.Spec, why string) (provided, consumed []string, err error) {
	roles := map[string]string{}
	for _, s := range specs {
		if r, ok := roles[s.Ref]; ok && r != s.Role {
			return nil, nil, fmt.Errorf("%s is used in both provides and consumes; split the schema (%s)", s.Ref, why)
		}
		roles[s.Ref] = s.Role
		if s.Role == "provided" {
			provided = append(provided, s.Ref)
		} else {
			consumed = append(consumed, s.Ref)
		}
	}
	return provided, consumed, nil
}

// Refs returns the distinct refs in specs, sorted: the role-agnostic
// counterpart of SplitRoles, for extractors whose source draws no
// input/output distinction.
func Refs(specs []extproto.Spec) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if !seen[s.Ref] {
			seen[s.Ref] = true
			out = append(out, s.Ref)
		}
	}
	sort.Strings(out)
	return out
}

func respond(schemas map[string]json.RawMessage, err error) int {
	resp := extproto.Response{SchemaVersion: extproto.SchemaVersion, Schemas: schemas}
	if err != nil {
		resp.Schemas = nil
		resp.Error = err.Error()
	}
	if resp.Schemas == nil {
		resp.Schemas = map[string]json.RawMessage{}
	}
	if e := json.NewEncoder(os.Stdout).Encode(resp); e != nil {
		fmt.Fprintln(os.Stderr, "wirefit extractor:", e)
		return 1
	}
	return 0
}
