package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wirefit/wirefit/internal/manifest"
)

const irRaw = `{"type":"object","properties":{"a":{"type":"string","x-ct-scalar":"string"}},"required":["a"]}`

func writeManifest(t *testing.T, dir, content string) (*manifest.Manifest, string) {
	t.Helper()
	p := filepath.Join(dir, "contracts.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return m, p
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestPublishPrunesDroppedInteractions(t *testing.T) {
	repo := t.TempDir() // no .git: write-only mode
	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	m, src := writeManifest(t, work, `
service: svc
schema-version: 1
provides:
  - id: svc.pong
    kind: rest
    direction: response
    dto: X
consumes:
  - id: prov.ping
    provider: prov
    dto: Y
`)
	provides := map[string][]byte{"svc.pong": []byte(irRaw)}
	consumes := map[string]map[string][]byte{"prov": {"prov.ping": []byte(irRaw)}}
	if err := st.Publish(m, src, provides, consumes, false); err != nil {
		t.Fatal(err)
	}
	provided := filepath.Join(repo, "contracts", "svc", "provides", "svc.pong.ir.json")
	consumed := filepath.Join(repo, "contracts", "svc", "consumes", "prov", "prov.ping.ir.json")
	for _, p := range []string{provided, consumed} {
		if !exists(t, p) {
			t.Fatalf("expected %s after first publish", p)
		}
	}

	// Republish with every interaction dropped: the store must unregister them.
	m2, src2 := writeManifest(t, work, "service: svc\nschema-version: 1\n")
	if err := st.Publish(m2, src2, map[string][]byte{}, map[string]map[string][]byte{}, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{provided, consumed,
		filepath.Join(repo, "contracts", "svc", "provides"),
		filepath.Join(repo, "contracts", "svc", "consumes")} {
		if exists(t, p) {
			t.Errorf("stale %s survived republish without the interaction", p)
		}
	}
	if !exists(t, filepath.Join(repo, "contracts", "svc", "manifest.yaml")) {
		t.Error("manifest copy must survive pruning")
	}
	if !exists(t, filepath.Join(repo, "contracts", "svc", "versions.json")) {
		t.Error("version log must survive pruning")
	}
}

func TestPublishPruneCommitsDeletions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	m, src := writeManifest(t, work, `
service: svc
schema-version: 1
consumes:
  - id: prov.ping
    provider: prov
    dto: Y
`)
	consumes := map[string]map[string][]byte{"prov": {"prov.ping": []byte(irRaw)}}
	if err := st.Publish(m, src, nil, consumes, false); err != nil {
		t.Fatal(err)
	}

	m2, src2 := writeManifest(t, work, "service: svc\nschema-version: 1\n")
	if err := st.Publish(m2, src2, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if out := git("status", "--porcelain", "--", "contracts"); out != "" {
		t.Errorf("pruned deletions must be committed, got dirty tree:\n%s", out)
	}

	// Identical republish stays an idempotent no-op.
	head := git("rev-parse", "HEAD")
	if err := st.Publish(m2, src2, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if git("rev-parse", "HEAD") != head {
		t.Error("republishing identical content must not create a commit")
	}
}

func TestConsumersOfSkipsUndeclaredProjection(t *testing.T) {
	repo := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declared := `
service: declared
schema-version: 1
consumes:
  - id: prov.ping
    provider: prov
    dto: Y
`
	write("contracts/declared/manifest.yaml", declared)
	write("contracts/declared/consumes/prov/prov.ping.ir.json", irRaw)
	// Stale projection from before pruning: file present, manifest dropped it.
	write("contracts/stale/manifest.yaml", "service: stale\nschema-version: 1\n")
	write("contracts/stale/consumes/prov/prov.ping.ir.json", irRaw)
	// No manifest copy at all: still counts (fail toward gating the provider).
	write("contracts/nomanifest/consumes/prov/prov.ping.ir.json", irRaw)

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.ConsumersOf("prov", "prov.ping")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"declared", "nomanifest"} {
		if _, ok := got[want]; !ok {
			t.Errorf("consumer %s missing from ConsumersOf", want)
		}
	}
	if _, ok := got["stale"]; ok {
		t.Error("projection undeclared by its published manifest must not count as a consumer")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 consumers, got %d: %v", len(got), got)
	}
}
