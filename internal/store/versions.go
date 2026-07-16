// Per-service publish counters: contracts/<service>/versions.json maps each
// interaction ref ("provides/<id>", "consumes/<provider>/<id>") to the ordered
// list of published hashes. A hash's version number is the 1-based index of its
// latest occurrence, so reports can label content-addressed blobs "v4" without
// touching git. The log is append-only: dropped interactions keep their history
// (a re-added ref continues its counter, numbers are never reused for different
// content), and a rollback republish appends a fresh number rather than reusing
// the old one, so a higher number always means a later publish.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// VersionLog is one service's publish history: ref → published hashes in order.
type VersionLog map[string][]string

func (s *Store) versionsPath(service string) string {
	return filepath.Join(s.serviceDir(service), "versions.json")
}

// LoadVersions returns a service's version log (empty when none exists).
func (s *Store) LoadVersions(service string) (VersionLog, error) {
	data, err := os.ReadFile(s.versionsPath(service))
	if os.IsNotExist(err) {
		return VersionLog{}, nil
	}
	if err != nil {
		return nil, err
	}
	var v VersionLog
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", s.versionsPath(service), err)
	}
	return v, nil
}

// SaveVersions writes the log canonically (sorted keys via marshalling).
func (s *Store) SaveVersions(service string, v VersionLog) error {
	if err := os.MkdirAll(filepath.Dir(s.versionsPath(service)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.versionsPath(service), append(data, '\n'), 0o644)
}

// Bump appends hash to ref's history unless it is already the latest entry,
// reporting whether the log changed. Only the last entry is compared: an
// idempotent republish is a no-op, a rollback to older content is a new event.
func (v VersionLog) Bump(ref, hash string) bool {
	h := v[ref]
	if len(h) > 0 && h[len(h)-1] == hash {
		return false
	}
	v[ref] = append(h, hash)
	return true
}

// Resolve returns the version number of a published hash: the 1-based index of
// its latest occurrence, or 0 when the ref or hash is unknown (published before
// version logs existed).
func (v VersionLog) Resolve(ref, hash string) int {
	h := v[ref]
	for i := len(h) - 1; i >= 0; i-- {
		if h[i] == hash {
			return i + 1
		}
	}
	return 0
}
