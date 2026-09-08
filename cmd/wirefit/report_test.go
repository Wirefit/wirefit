package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wirefit/wirefit/internal/store"
)

// checkArgs builds a service repo whose single consumed interaction matches
// promoRepo's published order-service, so cmdCheck reaches the report write
// with a clean verdict.
func checkArgs(t *testing.T) []string {
	t.Helper()
	st := promoRepo(t)
	dir := t.TempDir()
	mf := filepath.Join(dir, "contracts.yaml")
	if err := os.WriteFile(mf, []byte(`service: web-app
schema-version: 1
consumes:
  - id: orders.get
    provider: order-service
    dto: Y
`), 0o644); err != nil {
		t.Fatal(err)
	}
	irDir := filepath.Join(dir, "ir")
	writeRepoFile(t, irDir, filepath.Join("consumes", "order-service", "orders.get.ir.json"), irA)
	return []string{"-f", mf, "--contracts-repo", st.Dir, "--ir", irDir,
		"--overrides", filepath.Join(dir, "no-overrides.yaml")}
}

func canIDeployArgs(t *testing.T) []string {
	t.Helper()
	st := promoRepo(t)
	saveLock(t, st, "staging", store.EnvLock{
		"order-service": {RecordedAt: time.Now(), Provides: map[string]string{"orders.get": blob(t, st, irA)}},
	})
	saveLock(t, st, "prod", store.EnvLock{
		"web-app": {RecordedAt: time.Now(), Consumes: map[string]string{
			"order-service/orders.get": blob(t, st, irA),
		}},
	})
	return []string{"--contracts-repo", st.Dir, "--env", "prod",
		"--from-env", "staging", "--service", "order-service"}
}

// reportDests returns bad --report destinations that must fail on every
// platform, plus the read-only-directory case where the platform enforces it.
func reportDests(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dests := map[string]string{
		"parent is a regular file": filepath.Join(notADir, "report.md"),
		"path is a directory":      dir,
	}
	// Windows ignores the mode bits here, and root ignores them everywhere.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		dests["read-only directory"] = filepath.Join(ro, "report.md")
	}
	return dests
}

func TestCheckReportFailureExits2(t *testing.T) {
	t.Chdir(t.TempDir()) // check caches .wirefit/last-check.json in the cwd
	base := checkArgs(t)
	if code := cmdCheck(base); code != 0 {
		t.Fatalf("baseline check exit = %d, want 0", code)
	}
	for name, dest := range reportDests(t) {
		if code := cmdCheck(append(base, "--report", dest)); code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, code)
		}
	}
}

func TestCanIDeployReportFailureExits2(t *testing.T) {
	base := canIDeployArgs(t)
	if code := cmdCanIDeploy(base); code != 0 {
		t.Fatalf("baseline can-i-deploy exit = %d, want 0", code)
	}
	for name, dest := range reportDests(t) {
		if code := cmdCanIDeploy(append(base, "--report", dest)); code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, code)
		}
	}
}

// A report path in a directory that does not exist yet is created, not an error.
func TestReportCreatesMissingParents(t *testing.T) {
	t.Chdir(t.TempDir())
	dest := filepath.Join(t.TempDir(), "nested", "deeper", "report.md")
	if code := cmdCheck(append(checkArgs(t), "--report", dest)); code != 0 {
		t.Fatalf("check exit = %d, want 0", code)
	}
	if b, err := os.ReadFile(dest); err != nil || len(b) == 0 {
		t.Fatalf("report at %s: err = %v, len = %d, want a non-empty file", dest, err, len(b))
	}
	dest2 := filepath.Join(t.TempDir(), "nested", "report.md")
	if code := cmdCanIDeploy(append(canIDeployArgs(t), "--report", dest2)); code != 0 {
		t.Fatalf("can-i-deploy exit = %d, want 0", code)
	}
	if b, err := os.ReadFile(dest2); err != nil || len(b) == 0 {
		t.Fatalf("report at %s: err = %v, len = %d, want a non-empty file", dest2, err, len(b))
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The verdict still reaches the caller when only the report write fails: a
// broken --report path must not also hide the findings.
func TestReportFailureStillPrintsResults(t *testing.T) {
	t.Chdir(t.TempDir())
	args := append(checkArgs(t), "--report", t.TempDir())
	code := 0
	out := captureStdout(t, func() { code = cmdCheck(args) })
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(out, "orders.get") {
		t.Errorf("stdout = %q, want the check output to still name the interaction", out)
	}
}
