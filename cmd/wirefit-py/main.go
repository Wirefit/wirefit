// wirefit-py: the official Python extractor, an external executable
// speaking the public protocol (docs/extractor-protocol.md). Route to it per
// manifest: extractors: [{match: ".py", command: "wirefit-py"}].
package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/pytool"
)

func main() {
	opts, code := parse(os.Args[1:])
	if code != 0 {
		os.Exit(code)
	}
	os.Exit(extserve.Serve(func(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
		return extract(opts, projectDir, specs)
	}))
}

func parse(args []string) (pytool.RunOptions, int) {
	fs := flag.NewFlagSet("wirefit-py", flag.ContinueOnError)
	python := fs.String("python", "python3", "python binary")
	if fs.Parse(args) != nil {
		return pytool.RunOptions{}, 2
	}
	return pytool.RunOptions{PythonBin: *python}, 0
}

var runPython = pytool.Run

// Roles matter here: pydantic defaults affect requiredness differently in
// validation and serialization modes, so one ref cannot serve both sides.
func extract(opts pytool.RunOptions, projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
	provided, consumed, err := extserve.SplitRoles(specs, "pydantic io semantics differ per side")
	if err != nil {
		return nil, err
	}
	opts.ProjectDir = projectDir
	return runPython(opts, provided, consumed)
}
