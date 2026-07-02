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

	"github.com/wirefit/wirefit/internal/diff"
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

// ConsumersOf gathers every registered consumer projection for a provider
// interaction, with each consumer's unknown-fields strictness from its
// published manifest copy.
func (s *Store) ConsumersOf(provider, id string) (map[string]diff.Consumer, error) {
	root := filepath.Join(s.Dir, "contracts")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return map[string]diff.Consumer{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]diff.Consumer{}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == provider {
			continue
		}
		p := filepath.Join(root, e.Name(), "consumes", provider, id+".ir.json")
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		m, merr := s.ServiceManifest(e.Name())
		// A projection the published manifest no longer declares is a stale file
		// from before Publish pruned dropped interactions — not a registration.
		// A missing/unreadable manifest still counts: fail toward protecting the
		// provider gate rather than silently dropping a consumer.
		if merr == nil && m != nil && !m.ConsumesFrom(provider, id) {
			continue
		}
		sch, err := ir.Load(p)
		if err != nil {
			return nil, fmt.Errorf("consumer projection %s: %w", p, err)
		}
		reject := false
		if merr == nil && m != nil {
			reject = m.RejectsUnknown()
		}
		out[e.Name()] = diff.Consumer{Schema: sch, RejectUnknown: reject}
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
	if err := pruneStale(dir, provides, consumes); err != nil {
		return err
	}
	if noCommit {
		return nil
	}
	return s.commitAndPush(m.Service)
}

// pruneStale deletes IR files for interactions no longer in the manifest:
// publishing must unregister dropped provides/consumes, or a stale projection
// keeps blocking the provider forever as a phantom consumer.
func pruneStale(dir string, provides map[string][]byte, consumes map[string]map[string][]byte) error {
	provDir := filepath.Join(dir, "provides")
	entries, err := os.ReadDir(provDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".ir.json")
		if !ok {
			continue
		}
		if _, ok := provides[id]; !ok {
			if err := os.Remove(filepath.Join(provDir, e.Name())); err != nil {
				return err
			}
		}
	}
	removeIfEmpty(provDir)

	consRoot := filepath.Join(dir, "consumes")
	provs, err := os.ReadDir(consRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, p := range provs {
		if !p.IsDir() {
			continue
		}
		pd := filepath.Join(consRoot, p.Name())
		files, err := os.ReadDir(pd)
		if err != nil {
			return err
		}
		for _, f := range files {
			id, ok := strings.CutSuffix(f.Name(), ".ir.json")
			if !ok {
				continue
			}
			if _, ok := consumes[p.Name()][id]; !ok {
				if err := os.Remove(filepath.Join(pd, f.Name())); err != nil {
					return err
				}
			}
		}
		removeIfEmpty(pd)
	}
	removeIfEmpty(consRoot)
	return nil
}

// removeIfEmpty drops a directory once pruning emptied it, so the store
// layout never shows a service as providing/consuming nothing in particular.
func removeIfEmpty(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
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
