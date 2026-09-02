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

func gitAt(t *testing.T, repo string) func(...string) string {
	t.Helper()
	return func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func newTestGitRepo(t *testing.T) (string, func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	git := gitAt(t, repo)
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	return repo, git
}

func writeRepoFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireHeadPaths(t *testing.T, git func(...string) string, want ...string) {
	t.Helper()
	got := strings.Fields(git("show", "--format=", "--name-only", "HEAD"))
	wants := make(map[string]bool, len(want))
	for _, path := range want {
		wants[path] = true
	}
	if len(got) != len(wants) {
		t.Fatalf("HEAD paths = %v, want %v", got, want)
	}
	for _, path := range got {
		if !wants[path] {
			t.Fatalf("HEAD contains unexpected path %q; got %v, want %v", path, got, want)
		}
	}
}

func addBareRemote(t *testing.T, git func(...string) string) string {
	t.Helper()
	remote := t.TempDir()
	gitAt(t, remote)("init", "--bare", "-q", "-b", "main")
	git("remote", "add", "origin", remote)
	git("push", "-q", "-u", "origin", "main")
	return remote
}

func advanceRemote(t *testing.T, remote string) {
	t.Helper()
	peer := filepath.Join(t.TempDir(), "peer")
	out, err := exec.Command("git", "clone", "-q", remote, peer).CombinedOutput()
	if err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	git := gitAt(t, peer)
	git("config", "user.email", "peer@t")
	git("config", "user.name", "peer")
	writeRepoFile(t, peer, "remote-change.txt", "remote change\n")
	git("add", "remote-change.txt")
	git("commit", "-q", "-m", "remote change")
	git("push", "-q")
}

func installGitHook(t *testing.T, repo, name, message string) {
	t.Helper()
	p := filepath.Join(repo, ".git", "hooks", name)
	content := "#!/bin/sh\necho '" + message + "' >&2\nexit 1\n"
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
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
	repo, git := newTestGitRepo(t)

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

func TestPublishCommitsOnlyServicePaths(t *testing.T) {
	repo, git := newTestGitRepo(t)
	writeRepoFile(t, repo, "user-staged.txt", "base staged\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "base unstaged\n")
	git("add", "user-staged.txt", "user-unstaged.txt")
	git("commit", "-q", "-m", "baseline")

	writeRepoFile(t, repo, "user-staged.txt", "user staged change\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "user unstaged change\n")
	git("add", "user-staged.txt")

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
`)
	if err := st.Publish(m, src, map[string][]byte{"svc.pong": []byte(irRaw)}, nil, false); err != nil {
		t.Fatal(err)
	}

	for _, path := range strings.Fields(git("show", "--format=", "--name-only", "HEAD")) {
		if !strings.HasPrefix(path, "contracts/svc/") {
			t.Fatalf("publish commit contains unrelated path %q", path)
		}
	}
	if got := git("diff", "--cached", "--name-only"); got != "user-staged.txt" {
		t.Errorf("staged user changes = %q, want user-staged.txt", got)
	}
	if got := git("diff", "--name-only"); got != "user-unstaged.txt" {
		t.Errorf("unstaged user changes = %q, want user-unstaged.txt", got)
	}
	if got := git("show", "HEAD:user-staged.txt"); got != "base staged" {
		t.Errorf("publish committed staged user content %q", got)
	}
	if got := git("show", "HEAD:user-unstaged.txt"); got != "base unstaged" {
		t.Errorf("publish committed unstaged user content %q", got)
	}

	head := git("rev-parse", "HEAD")
	if err := st.Publish(m, src, map[string][]byte{"svc.pong": []byte(irRaw)}, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := git("rev-parse", "HEAD"); got != head {
		t.Errorf("identical publish created commit %s, previous HEAD %s", got, head)
	}
}

func TestCommitPathsCommitsOnlyRequestedPaths(t *testing.T) {
	repo, git := newTestGitRepo(t)
	writeRepoFile(t, repo, "user-staged.txt", "base staged\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "base unstaged\n")
	git("add", "user-staged.txt", "user-unstaged.txt")
	git("commit", "-q", "-m", "baseline")

	writeRepoFile(t, repo, "user-staged.txt", "user staged change\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "user unstaged change\n")
	git("add", "user-staged.txt")
	writeRepoFile(t, repo, "_envs/production.lock.json", "{}\n")
	writeRepoFile(t, repo, "_blobs/abc.ir.json", irRaw+"\n")

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitPaths("wirefit record-deploy: svc → production", "_envs", "_blobs"); err != nil {
		t.Fatal(err)
	}
	requireHeadPaths(t, git, "_blobs/abc.ir.json", "_envs/production.lock.json")
	if got := git("diff", "--cached", "--name-only"); got != "user-staged.txt" {
		t.Errorf("staged user changes = %q, want user-staged.txt", got)
	}
	if got := git("diff", "--name-only"); got != "user-unstaged.txt" {
		t.Errorf("unstaged user changes = %q, want user-unstaged.txt", got)
	}

	head := git("rev-parse", "HEAD")
	if err := st.CommitPaths("no scoped changes", "_envs", "_blobs"); err != nil {
		t.Fatal(err)
	}
	if got := git("rev-parse", "HEAD"); got != head {
		t.Errorf("unrelated changes caused commit %s, previous HEAD %s", got, head)
	}
	if err := st.CommitPaths("missing scope"); err == nil {
		t.Error("CommitPaths accepted an empty path list")
	}
}

func TestCommitPathsRebasesAndRetriesWithUserChanges(t *testing.T) {
	repo, git := newTestGitRepo(t)
	writeRepoFile(t, repo, "user-staged.txt", "base staged\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "base unstaged\n")
	git("add", "user-staged.txt", "user-unstaged.txt")
	git("commit", "-q", "-m", "baseline")
	remote := addBareRemote(t, git)
	advanceRemote(t, remote)

	writeRepoFile(t, repo, "user-staged.txt", "user staged change\n")
	writeRepoFile(t, repo, "user-unstaged.txt", "user unstaged change\n")
	git("add", "user-staged.txt")
	writeRepoFile(t, repo, "_envs/production.lock.json", "{}\n")
	writeRepoFile(t, repo, "_blobs/abc.ir.json", irRaw+"\n")

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitPaths("wirefit record-deploy: svc → production", "_envs", "_blobs"); err != nil {
		t.Fatal(err)
	}
	if local, pushed := git("rev-parse", "HEAD"), gitAt(t, remote)("rev-parse", "refs/heads/main"); local != pushed {
		t.Errorf("local HEAD %s was not pushed; remote is %s", local, pushed)
	}
	requireHeadPaths(t, git, "_blobs/abc.ir.json", "_envs/production.lock.json")
	if got := git("show", "HEAD:user-staged.txt"); got != "base staged" {
		t.Errorf("retry committed staged user content %q", got)
	}
	if got := git("show", "HEAD:user-unstaged.txt"); got != "base unstaged" {
		t.Errorf("retry committed unstaged user content %q", got)
	}
	status := git("status", "--porcelain")
	for _, path := range []string{"user-staged.txt", "user-unstaged.txt"} {
		if !strings.Contains(status, path) {
			t.Errorf("retry lost user change %s; status:\n%s", path, status)
		}
	}
}

func TestCommitPathsPreservesPushOutput(t *testing.T) {
	repo, git := newTestGitRepo(t)
	writeRepoFile(t, repo, "seed.txt", "seed\n")
	git("add", "seed.txt")
	git("commit", "-q", "-m", "baseline")
	addBareRemote(t, git)
	installGitHook(t, repo, "pre-push", "push rejected by test")
	writeRepoFile(t, repo, "_envs/production.lock.json", "{}\n")

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CommitPaths("deploy", "_envs")
	if err == nil || !strings.Contains(err.Error(), "push rejected by test") {
		t.Fatalf("push error = %v, want hook output", err)
	}
}

func TestCommitPathsPreservesRebaseOutput(t *testing.T) {
	repo, git := newTestGitRepo(t)
	writeRepoFile(t, repo, "seed.txt", "seed\n")
	git("add", "seed.txt")
	git("commit", "-q", "-m", "baseline")
	remote := addBareRemote(t, git)
	advanceRemote(t, remote)
	installGitHook(t, repo, "pre-rebase", "rebase rejected by test")
	writeRepoFile(t, repo, "_envs/production.lock.json", "{}\n")

	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CommitPaths("deploy", "_envs")
	if err == nil || !strings.Contains(err.Error(), "rebase rejected by test") {
		t.Fatalf("rebase error = %v, want hook output", err)
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
