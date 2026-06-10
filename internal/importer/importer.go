// Package importer ingests schema-native artifacts directly (Phase 5):
// where a real schema file exists (.proto, .avsc, GraphQL SDL), it IS the
// source of truth — extract from it, never from code (SPEC §10). Importers
// are IR producers like extractors; the diff engine is untouched.
package importer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IsSpec reports whether a dto reference targets an importer, by file suffix.
func IsSpec(dto string) bool {
	file, _, _ := strings.Cut(dto, "#")
	for _, suf := range []string{".proto", ".avsc", ".graphql", ".gql"} {
		if strings.HasSuffix(file, suf) {
			return true
		}
	}
	return false
}

// Options carries importer-relevant manifest settings.
type Options struct {
	// GraphQLSchema: SDL path used to resolve operation files (PRD 5.4).
	GraphQLSchema string
}

// Import resolves one spec ("file#TypeName", or a bare operation file for
// GraphQL persisted queries) into raw IR JSON.
func Import(projectDir, spec string, opts Options) (json.RawMessage, error) {
	file, sel, _ := strings.Cut(spec, "#")
	switch {
	case strings.HasSuffix(file, ".proto"):
		return importProto(projectDir, file, sel)
	case strings.HasSuffix(file, ".avsc"):
		return importAvro(projectDir, file, sel)
	case strings.HasSuffix(file, ".graphql"), strings.HasSuffix(file, ".gql"):
		if sel != "" {
			return importGraphQLType(projectDir, file, sel)
		}
		return importGraphQLOperation(projectDir, file, opts.GraphQLSchema)
	}
	return nil, fmt.Errorf("no importer for %s", spec)
}

// node helpers shared by all importers ---------------------------------------

type node = map[string]any

func scalarNode(s string) node {
	jt := map[string]string{"bool": "boolean", "int32": "integer", "int64": "integer",
		"float32": "number", "float64": "number", "decimal": "number"}[s]
	if jt == "" {
		jt = "string"
	}
	return node{"type": jt, "x-ct-scalar": s}
}

func marshal(n node) (json.RawMessage, error) {
	b, err := json.Marshal(n)
	return json.RawMessage(b), err
}
