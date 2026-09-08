// Package extproto defines the public extractor protocol v1 (PRD 3.2): the
// community surface for third-party language extractors. An extractor is any
// executable that reads a Request as JSON on stdin and writes a Response as
// JSON on stdout. Frozen at schemaVersion 1; evolution is additive only.
//
// Full specification: docs/extractor-protocol.md.
package extproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const SchemaVersion = 1

// The only two roles a spec may carry (docs/extractor-protocol.md).
const (
	RoleProvided = "provided"
	RoleConsumed = "consumed"
)

func ValidRole(role string) bool { return role == RoleProvided || role == RoleConsumed }

// Spec is one DTO reference to extract.
type Spec struct {
	// Ref is the manifest dto reference, e.g. "src/models.py#OrderView".
	Ref string `json:"ref"`
	// Role is "provided" (the service emits this shape) or "consumed" (the
	// service parses this shape). Extractors whose source distinguishes
	// input/output semantics (defaults, transforms) must honor it.
	Role string `json:"role"`
}

type Request struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProjectDir    string `json:"projectDir"`
	Specs         []Spec `json:"specs"`
}

type Response struct {
	SchemaVersion int `json:"schemaVersion"`
	// Schemas maps each requested Ref to its wirefit IR document.
	Schemas map[string]json.RawMessage `json:"schemas"`
	// Error: fatal extraction failure (unsupported shape etc.) — the message
	// must name the construct and location (fail loudly, never guess).
	Error string `json:"error,omitempty"`
}

// Invoke runs an external extractor executable with the protocol.
func Invoke(command []string, req Request) (*Response, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("empty extractor command")
	}
	for _, s := range req.Specs {
		if !ValidRole(s.Role) {
			return nil, fmt.Errorf("extractor %s: %s: role %q must be %q or %q", command[0], s.Ref, s.Role, RoleProvided, RoleConsumed)
		}
	}
	// The extractor runs in the service project and is promised an absolute
	// projectDir: a relative one would otherwise resolve against that new
	// working directory rather than against wirefit's.
	dir, err := filepath.Abs(req.ProjectDir)
	if err != nil {
		return nil, err
	}
	req.ProjectDir = dir
	req.SchemaVersion = SchemaVersion
	in, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(in)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("extractor %s failed: %w", command[0], err)
	}
	var resp Response
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("extractor %s: invalid response JSON: %w", command[0], err)
	}
	if resp.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("extractor %s: protocol version %d, want %d", command[0], resp.SchemaVersion, SchemaVersion)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("extractor %s: %s", command[0], resp.Error)
	}
	return &resp, nil
}
