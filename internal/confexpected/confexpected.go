// Package confexpected embeds the conformance corpus's expected IR documents
// (one per logical case) into the wirefit binary, so `wirefit extractor-test`
// can verify third-party extractors offline (PRD 3.2).
//
// Files are written by `conformance/run.sh --update-expected` and verified on
// every corpus run — they are the cross-language source of truth.
package confexpected

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed expected/*.ir.json
var files embed.FS

// Get returns the expected IR document for a corpus case name.
func Get(name string) ([]byte, error) {
	b, err := files.ReadFile("expected/" + name + ".ir.json")
	if err != nil {
		return nil, fmt.Errorf("unknown corpus case %q (have: %s)", name, strings.Join(Names(), ", "))
	}
	return b, nil
}

// Names lists all corpus case names.
func Names() []string {
	entries, _ := fs.ReadDir(files, "expected")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".ir.json"))
	}
	sort.Strings(out)
	return out
}
