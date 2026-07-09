// wirefit-py: the official Python extractor, an external executable
// speaking the public protocol (docs/extractor-protocol.md). Route to it per
// manifest: extractors: [{match: ".py", command: "wirefit-py"}].
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/pytool"
)

type options struct {
	python string
}

var runPython = pytool.Run

func main() {
	opts, code := parse(os.Args[1:])
	if code != 0 {
		os.Exit(code)
	}
	os.Exit(extserve.Serve(func(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
		return extract(opts, projectDir, specs)
	}))
}

func parse(args []string) (options, int) {
	fs := flag.NewFlagSet("wirefit-py", flag.ContinueOnError)
	python := fs.String("python", "python3", "python binary")
	if fs.Parse(args) != nil {
		return options{}, 2
	}
	return options{python: *python}, 0
}

// Roles matter here: pydantic defaults affect requiredness differently in
// validation and serialization modes, so one ref cannot serve both sides.
func extract(opts options, projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
	var provided, consumed []string
	roles := map[string]string{}
	for _, s := range specs {
		if r, ok := roles[s.Ref]; ok && r != s.Role {
			return nil, fmt.Errorf("%s is used in both provides and consumes; split the schema (pydantic io semantics differ per side)", s.Ref)
		}
		roles[s.Ref] = s.Role
		if s.Role == "provided" {
			provided = append(provided, s.Ref)
		} else {
			consumed = append(consumed, s.Ref)
		}
	}
	return runPython(pytool.RunOptions{ProjectDir: projectDir, PythonBin: opts.python}, provided, consumed)
}
