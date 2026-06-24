// Env lockfiles + content-addressed IR blobs (Phase 4, PRD 4.1/4.2).
//
//	_envs/<env>.lock.json   what is DEPLOYED where (per interaction, by hash)
//	_blobs/<hex>.ir.json    canonical IR bytes, content-addressed
//
// Design deviation from the PRD (recorded): blobs replace the proposed
// `_index/` + git-history lookup — simpler, content-deduped, and the store
// is a git repo anyway so history is preserved for free.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wirefit/wirefit/internal/ir"
)

// ServiceLock records one service's deployed contract state in one env.
type ServiceLock struct {
	RecordedAt time.Time `json:"recordedAt"`
	RecordedBy string    `json:"recordedBy"`
	// Provides: interaction id → "sha256:<hex>" of the deployed IR.
	Provides map[string]string `json:"provides,omitempty"`
	// Consumes: "<provider>/<interaction id>" → hash of the deployed usage projection.
	Consumes map[string]string `json:"consumes,omitempty"`
}

// EnvLock is the full lockfile for one environment: service → state.
type EnvLock map[string]*ServiceLock

func (s *Store) envLockPath(env string) string {
	return filepath.Join(s.Dir, "_envs", env+".lock.json")
}

// LoadEnvLock returns the lockfile for an env (empty when none exists).
func (s *Store) LoadEnvLock(env string) (EnvLock, error) {
	data, err := os.ReadFile(s.envLockPath(env))
	if os.IsNotExist(err) {
		return EnvLock{}, nil
	}
	if err != nil {
		return nil, err
	}
	var l EnvLock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", s.envLockPath(env), err)
	}
	return l, nil
}

// SaveEnvLock writes the lockfile canonically (sorted keys via marshalling).
func (s *Store) SaveEnvLock(env string, l EnvLock) error {
	if err := os.MkdirAll(filepath.Dir(s.envLockPath(env)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.envLockPath(env), append(data, '\n'), 0o644)
}

// Envs lists environments with lockfiles.
func (s *Store) Envs() []string {
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "_envs"))
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lock.json") {
			out = append(out, strings.TrimSuffix(e.Name(), ".lock.json"))
		}
	}
	sort.Strings(out)
	return out
}

// WriteBlob stores canonical IR bytes content-addressed; returns "sha256:<hex>".
func (s *Store) WriteBlob(raw []byte) (string, error) {
	canon, err := ir.Canonicalize(raw)
	if err != nil {
		return "", err
	}
	hash, err := ir.Hash(canon)
	if err != nil {
		return "", err
	}
	p := filepath.Join(s.Dir, "_blobs", strings.TrimPrefix(hash, "sha256:")+".ir.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		// Stored pretty-printed for readability; the address (hash) is over the compact form.
		pretty, perr := ir.CanonicalIndent(canon)
		if perr != nil {
			return "", perr
		}
		if err := os.WriteFile(p, append(pretty, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	return hash, nil
}

// ReadBlob loads the IR document with the given "sha256:<hex>" hash.
func (s *Store) ReadBlob(hash string) (*ir.Schema, error) {
	p := filepath.Join(s.Dir, "_blobs", strings.TrimPrefix(hash, "sha256:")+".ir.json")
	sch, err := ir.Load(p)
	if err != nil {
		return nil, fmt.Errorf("blob %s: %w (was it published before recording the deploy?)", hash, err)
	}
	return sch, nil
}

// CommitPaths commits (and pushes, when a remote exists) arbitrary repo paths.
func (s *Store) CommitPaths(msg string, paths ...string) error {
	if _, err := os.Stat(filepath.Join(s.Dir, ".git")); os.IsNotExist(err) {
		return nil // plain directory: write-only mode (demos, tests)
	}
	if _, err := s.git(append([]string{"add", "-A"}, paths...)...); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if out, _ := s.git("status", "--porcelain"); out == "" {
		return nil
	}
	if out, err := s.git("commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %s: %w", out, err)
	}
	if remotes, _ := s.git("remote"); remotes == "" {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := s.git("push"); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("git push: %w", err)
		}
		if out, err := s.git("pull", "--rebase"); err != nil {
			return fmt.Errorf("git pull --rebase: %s: %w", out, err)
		}
	}
	return lastErr
}
