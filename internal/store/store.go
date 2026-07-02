// Package store implements the git-backed contracts repository (SPEC §9).
//
// Layout inside the contracts repo working copy:
//
//	contracts/<service>/manifest.yaml
//	contracts/<service>/provides/<interaction-id>.ir.json
//	contracts/<service>/consumes/<provider>/<interaction-id>.ir.json
//
// The store is a plain working copy: `Publish` writes files and, when the
// directory is a git repo with a remote, commits and pushes with a
// pull-rebase retry (PRD 1.8). Reading never touches git.
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wirefit/wirefit/internal/ir"
	"github.com/wirefit/wirefit/internal/manifest"
)

type Store struct {
	Dir string // contracts repo working copy
}

func Open(dir string) (*Store, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("contracts repo: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("contracts repo: %s is not a directory", dir)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) serviceDir(service string) string {
	return filepath.Join(s.Dir, "contracts", service)
}

// ProviderIR returns the published schema for a provider interaction,
// with ok=false when it has never been published.
func (s *Store) ProviderIR(service, id string) (*ir.Schema, bool, error) {
	p := filepath.Join(s.serviceDir(service), "provides", id+".ir.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, false, nil
	}
	sch, err := ir.Load(p)
	if err != nil {
		return nil, false, fmt.Errorf("published IR %s: %w", p, err)
	}
	return sch, true, nil
}

// ServiceManifest returns the manifest copy a service published, or nil.
func (s *Store) ServiceManifest(service string) (*manifest.Manifest, error) {
	p := filepath.Join(s.serviceDir(service), "manifest.yaml")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}
	return manifest.Load(p)
}

// Consumer is one registered consumer's projection of a provider interaction,
// with the unknown-fields strictness read from its published manifest copy. It
// is the storage layer's own type so the store does not depend on internal/diff;
// callers translate it into a diff.Consumer when running a check.
type Consumer struct {
	Schema        *ir.Schema
	RejectUnknown bool
}

// ConsumersOf gathers every registered consumer projection for a provider
// interaction, with each consumer's unknown-fields strictness from its
// published manifest copy.
func (s *Store) ConsumersOf(provider, id string) (map[string]Consumer, error) {
	root := filepath.Join(s.Dir, "contracts")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return map[string]Consumer{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Consumer{}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == provider {
			continue
		}
		p := filepath.Join(root, e.Name(), "consumes", provider, id+".ir.json")
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		sch, err := ir.Load(p)
		if err != nil {
			return nil, fmt.Errorf("consumer projection %s: %w", p, err)
		}
		reject := false
		if m, err := s.ServiceManifest(e.Name()); err == nil && m != nil {
			reject = m.RejectsUnknown()
		}
		out[e.Name()] = Consumer{Schema: sch, RejectUnknown: reject}
	}
	return out, nil
}

// Publish writes a service's manifest copy and canonicalized IR files, then
// commits (and pushes, when a remote exists) unless noCommit is set.
//
// provides:  interaction id → IR bytes
// consumes:  provider → interaction id → IR bytes
func (s *Store) Publish(m *manifest.Manifest, manifestSrc string,
	provides map[string][]byte, consumes map[string]map[string][]byte, noCommit bool) error {

	dir := s.serviceDir(m.Service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src, err := os.ReadFile(manifestSrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), src, 0o644); err != nil {
		return err
	}
	write := func(rel string, raw []byte) error {
		canon, err := ir.Canonicalize(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		// Content-addressed copy for env lockfile resolution (Phase 4).
		if _, err := s.WriteBlob(canon); err != nil {
			return err
		}
		// Stored pretty-printed for readability; the hash is over the compact form.
		pretty, err := ir.CanonicalIndent(canon)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		return os.WriteFile(p, append(pretty, '\n'), 0o644)
	}
	for id, raw := range provides {
		if err := write(filepath.Join("provides", id+".ir.json"), raw); err != nil {
			return err
		}
	}
	for prov, byID := range consumes {
		for id, raw := range byID {
			if err := write(filepath.Join("consumes", prov, id+".ir.json"), raw); err != nil {
				return err
			}
		}
	}
	if noCommit {
		return nil
	}
	return s.commitAndPush(m.Service)
}

func (s *Store) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", s.Dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *Store) commitAndPush(service string) error {
	if _, err := os.Stat(filepath.Join(s.Dir, ".git")); os.IsNotExist(err) {
		return nil // plain directory: write-only mode (demos, tests)
	}
	if _, err := s.git("add", "-A", filepath.Join("contracts", service)); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	// No staged changes for THIS service → idempotent no-op (re-publishing identical
	// content). We check the staged diff for the service path, not whole-repo status,
	// so an otherwise-dirty repo (untracked _blobs, other services) doesn't make the
	// commit fail with "nothing to commit".
	if _, err := s.git("diff", "--cached", "--quiet", "--", filepath.Join("contracts", service)); err == nil {
		return nil
	}
	if out, err := s.git("commit", "-m", "wirefit publish: "+service); err != nil {
		return fmt.Errorf("git commit: %s: %w", out, err)
	}
	if remotes, _ := s.git("remote"); remotes == "" {
		return nil // local-only repo
	}
	// Push with pull-rebase retry against concurrent publishes (PRD 1.8).
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if out, err := s.git("push"); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("git push: %s: %w", out, err)
		}
		if out, err := s.git("pull", "--rebase"); err != nil {
			return fmt.Errorf("git pull --rebase: %s: %w", out, err)
		}
	}
	return lastErr
}
