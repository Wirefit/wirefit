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
