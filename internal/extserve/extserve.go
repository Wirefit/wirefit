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

// SplitRoles partitions specs by role for a role-sensitive extractor, one whose
// source gives the two sides different io semantics. Such a ref cannot serve
// both sides, so a ref used in both provides and consumes is an error; why
// names the difference (e.g. "zod io semantics differ per side"). The dual-role
// rejection is one protocol behaviour: a private copy per binary drifts.
func SplitRoles(specs []extproto.Spec, why string) (provided, consumed []string, err error) {
	roles := map[string]string{}
	for _, s := range specs {
		if r, ok := roles[s.Ref]; ok {
			if r != s.Role {
				return nil, nil, fmt.Errorf("%s is used in both provides and consumes; split the schema (%s)", s.Ref, why)
			}
			continue
		}
		roles[s.Ref] = s.Role
		if s.Role == extproto.RoleProvided {
			provided = append(provided, s.Ref)
		} else {
			consumed = append(consumed, s.Ref)
		}
	}
	return provided, consumed, nil
}

// Refs returns the distinct refs in specs, sorted, for a role-agnostic
// extractor: its source draws no io distinction, so a ref used on both sides
// extracts once.
func Refs(specs []extproto.Spec) []string {
	seen := map[string]bool{}
	refs := make([]string, 0, len(specs))
	for _, s := range specs {
		if !seen[s.Ref] {
			seen[s.Ref] = true
			refs = append(refs, s.Ref)
		}
	}
	sort.Strings(refs)
	return refs
}

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
	for _, spec := range req.Specs {
		if !extproto.ValidRole(spec.Role) {
			return respond(nil, fmt.Errorf("%s: role %q must be %q or %q", spec.Ref, spec.Role, extproto.RoleProvided, extproto.RoleConsumed))
		}
	}
	schemas, err := fn(req.ProjectDir, req.Specs)
	return respond(schemas, err)
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
