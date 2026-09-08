// Package manifest parses and validates contracts.yaml (SPEC §5) — the one
// piece of per-service configuration ct requires.
package manifest

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wirefit/wirefit/internal/yamlx"
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
	Match   string  `yaml:"match"`
	Command Command `yaml:"command"` // argv; argv[0] is PATH-resolved, run in the service repo
}

// Command is an extractor argv. YAML accepts a plain string, split on
// whitespace ("wirefit-py --python .venv/bin/python"), or an explicit
// sequence — the only form that can carry an argument containing spaces.
type Command []string

func (c *Command) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*c = strings.Fields(s)
		return nil
	case yaml.SequenceNode:
		var argv []string
		if err := n.Decode(&argv); err != nil {
			return err
		}
		*c = argv
		return nil
	}
	return fmt.Errorf("line %d: command must be a string or a list of arguments", n.Line)
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

// ValidateServiceName checks a service name before it is used as a contracts
// repository path component. Keeping this rule exported lets CLI flags use
// the exact same validation as manifest service names.
func ValidateServiceName(name string) error {
	if !serviceRe.MatchString(name) {
		return fmt.Errorf("service %q must match %s", name, serviceRe)
	}
	return nil
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yamlx.StrictUnmarshal(data, &m); err != nil {
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

	if err := ValidateServiceName(m.Service); err != nil {
		fail("%v", err)
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
		if len(x.Command) == 0 || x.Command[0] == "" {
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
