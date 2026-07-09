// Package manifest parses and validates contracts.yaml (SPEC §5) — the one
// piece of per-service configuration ct requires.
package manifest

import (
	"bytes"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Service       string        `yaml:"service"`
	SchemaVersion int           `yaml:"schema-version"`
	Provides      []Interaction `yaml:"provides"`
	Consumes      []Consumption `yaml:"consumes"`
	Settings      Settings      `yaml:"settings"`
	// Extractors routes dto references to third-party extractor executables
	// implementing the public protocol (PRD 3.2, docs/extractor-protocol.md).
	Extractors []ExternalExtractor `yaml:"extractors"`
}

// ExternalExtractor routes dto references by file suffix to a command.
type ExternalExtractor struct {
	// Match: a file suffix like ".py", or "*", the single fallback for
	// suffix-less refs (java FQNs), consulted after the built-in routes.
	Match   string `yaml:"match"`
	Command string `yaml:"command"` // executable (PATH-resolved), run in the service repo
}

type Interaction struct {
	ID        string `yaml:"id"`
	Kind      string `yaml:"kind"`      // rest | event | rpc
	Direction string `yaml:"direction"` // response | request | event
	DTO       string `yaml:"dto"`
	// Schema: optional schema-native artifact (.proto/.avsc/.graphql). Alone,
	// it IS the contract source; together with dto it arms the mirror check
	// (PRD 5.7): code and schema file must agree, drift always fails.
	Schema string `yaml:"schema"`
}

type Consumption struct {
	ID       string `yaml:"id"`
	Provider string `yaml:"provider"`
	DTO      string `yaml:"dto"`
}

type Settings struct {
	// UnknownFields: "" (default, = ignore) | ignore | reject (SPEC C5).
	UnknownFields string `yaml:"unknown-fields"`
	// JavaMapper: deprecated and unused: pass --mapper on the wirefit-java
	// extractor command instead. Still parsed and validated so old manifests
	// warn rather than break.
	JavaMapper string `yaml:"java-mapper"`
	// GraphQLSchema: SDL path used to resolve GraphQL operation files (PRD 5.4).
	GraphQLSchema string `yaml:"graphql-schema"`
}

var (
	serviceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	idRe      = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	mapperRe  = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)+#[A-Za-z_$][\w$]*$`)
)

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("contracts.yaml: %w", err)
	}
	return &m, nil
}

// Validate returns every problem found (not just the first) so a developer
// fixes the manifest in one pass (NF1).
func (m *Manifest) Validate() []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if !serviceRe.MatchString(m.Service) {
		fail("service: %q must match %s", m.Service, serviceRe)
	}
	if m.SchemaVersion != 1 {
		fail("schema-version: must be 1, got %d", m.SchemaVersion)
	}
	switch m.Settings.UnknownFields {
	case "", "ignore", "reject":
	default:
		fail("settings.unknown-fields: %q must be ignore or reject", m.Settings.UnknownFields)
	}
	if m.Settings.JavaMapper != "" && !mapperRe.MatchString(m.Settings.JavaMapper) {
		fail("settings.java-mapper: %q must be <class-fqn>#<static-method>", m.Settings.JavaMapper)
	}
	wildcards := 0
	for i, x := range m.Extractors {
		switch {
		case x.Match == "*":
			// One fallback only: two wildcards would race for every
			// suffix-less ref with registry order as the tiebreaker.
			wildcards++
			if wildcards > 1 {
				fail("extractors[%d]: only one \"*\" fallback entry is allowed", i)
			}
		case len(x.Match) < 2 || x.Match[0] != '.':
			fail("extractors[%d]: match must be a file suffix like .py, or \"*\" for suffix-less refs, got %q", i, x.Match)
		}
		if x.Command == "" {
			fail("extractors[%d]: command is required", i)
		}
	}

	seen := map[string]bool{}
	for i, p := range m.Provides {
		at := fmt.Sprintf("provides[%d] (%s)", i, p.ID)
		if !idRe.MatchString(p.ID) {
			fail("%s: id must be dot-namespaced lowercase (e.g. orders.get-order), got %q", at, p.ID)
		}
		if seen[p.ID] {
			fail("%s: duplicate interaction id", at)
		}
		seen[p.ID] = true
		switch p.Kind {
		case "rest", "event", "rpc":
		default:
			fail("%s: kind %q must be rest, event or rpc", at, p.Kind)
		}
		switch p.Direction {
		case "response", "request", "event":
		default:
			fail("%s: direction %q must be response, request or event", at, p.Direction)
		}
		if p.Kind == "event" && p.Direction != "event" {
			fail("%s: kind event requires direction event", at)
		}
		if p.DTO == "" && p.Schema == "" {
			fail("%s: dto or schema is required", at)
		}
	}

	seenC := map[string]bool{}
	for i, c := range m.Consumes {
		at := fmt.Sprintf("consumes[%d] (%s)", i, c.ID)
		if !idRe.MatchString(c.ID) {
			fail("%s: id must be dot-namespaced lowercase, got %q", at, c.ID)
		}
		key := c.Provider + "/" + c.ID
		if seenC[key] {
			fail("%s: duplicate consumption of %s", at, key)
		}
		seenC[key] = true
		if !serviceRe.MatchString(c.Provider) {
			fail("%s: provider %q must match %s", at, c.Provider, serviceRe)
		}
		if c.Provider == m.Service {
			fail("%s: a service cannot consume from itself", at)
		}
		if c.DTO == "" {
			fail("%s: dto is required", at)
		}
	}
	return errs
}

// RejectsUnknown reports the effective unknown-fields strictness.
func (m *Manifest) RejectsUnknown() bool { return m.Settings.UnknownFields == "reject" }

// ConsumesFrom reports whether the manifest declares consumption of the
// provider's interaction.
func (m *Manifest) ConsumesFrom(provider, id string) bool {
	for _, c := range m.Consumes {
		if c.Provider == provider && c.ID == id {
			return true
		}
	}
	return false
}
