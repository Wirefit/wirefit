// Package yamlx wraps gopkg.in/yaml.v3 with the strictness every wirefit
// config loader needs. A bare Decode or Unmarshal reads only the first
// document and silently drops the rest, so a stray "---" would quietly
// discard half of a contracts.yaml, policy or pipeline file.
package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// StrictUnmarshal decodes exactly one YAML document into dst, rejecting both
// unknown fields and any trailing document.
func StrictUnmarshal(data []byte, dst any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var rest yaml.Node
	err := dec.Decode(&rest)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple YAML documents are not supported")
}
