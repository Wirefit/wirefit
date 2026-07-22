// Promotion pipeline (_envs/pipeline.yaml): the declared order environments
// promote through (dev → staging → prod). Envs() globs only *.lock.json, so
// the file coexists with the lockfiles it orders.
package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Pipeline is the parsed _envs/pipeline.yaml.
type Pipeline struct {
	SchemaVersion int      `yaml:"schema-version"`
	Envs          []string `yaml:"envs"`
}

func (s *Store) pipelinePath() string {
	return filepath.Join(s.Dir, "_envs", "pipeline.yaml")
}

// LoadPipeline returns the declared promotion order (each env promotes into
// the next), or nil when no _envs/pipeline.yaml exists.
func (s *Store) LoadPipeline() ([]string, error) {
	data, err := os.ReadFile(s.pipelinePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Pipeline
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", s.pipelinePath(), err)
	}
	if p.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: schema-version must be 1, got %d", s.pipelinePath(), p.SchemaVersion)
	}
	if err := ValidatePipelineEnvs(p.Envs); err != nil {
		return nil, fmt.Errorf("%s: %w", s.pipelinePath(), err)
	}
	return p.Envs, nil
}

// Env names become lockfile names; constrain them like service names.
var envRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidatePipelineEnvs rejects orders that cannot describe a promotion path:
// fewer than two envs, duplicates, or names that cannot be lockfile names.
// Shared by LoadPipeline and the `matrix --envs` override.
func ValidatePipelineEnvs(envs []string) error {
	if len(envs) < 2 {
		return fmt.Errorf("a pipeline needs at least 2 envs, got %d", len(envs))
	}
	seen := map[string]bool{}
	for _, e := range envs {
		if !envRe.MatchString(e) {
			return fmt.Errorf("env %q must match %s", e, envRe)
		}
		if seen[e] {
			return fmt.Errorf("duplicate env %q", e)
		}
		seen[e] = true
	}
	return nil
}
