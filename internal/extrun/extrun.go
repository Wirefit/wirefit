// Package extrun holds the subprocess + cache plumbing shared inside the
// extractor processes: gotool in the core, and tstool, javatool and pytool
// inside the wirefit-ts, wirefit-java and wirefit-py binaries. It does not
// speak the extproto wire protocol: the engines pass specs as CLI args and
// their child emits a bare IR map.
package extrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Run executes a built-in extractor subprocess that emits a JSON object mapping
// each requested spec to its IR document, and returns it. stderr is forwarded so
// the extractor's own diagnostics reach the user; name (e.g. "go", "ts", "java")
// labels the error messages. The caller sets cmd.Dir / cmd.Stdin as needed.
func Run(name string, cmd *exec.Cmd) (map[string]json.RawMessage, error) {
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s extractor failed: %w", name, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("bad %s extractor output: %w", name, err)
	}
	return m, nil
}

// UserCacheDir may be replaced by nonparallel tests; restore it with t.Cleanup.
var UserCacheDir = os.UserCacheDir

// CacheDir returns <UserCacheDir>/wirefit/<name>/<version>, created. name is the
// per-extractor cache namespace (e.g. "java-extractor"); version keys the cache
// so a bump invalidates stale compiled/installed artifacts.
func CacheDir(name, version string) (string, error) {
	base, err := UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "wirefit", name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// CachePath keys immutable artifacts by their source and dependency inputs.
func CachePath(root string, inputs ...string) string {
	h := sha256.New()
	for _, s := range inputs {
		fmt.Fprintf(h, "%d:", len(s))
		h.Write([]byte(s))
	}
	return filepath.Join(root, "artifacts-"+hex.EncodeToString(h.Sum(nil)))
}

// WithTempDir isolates preparation and removes only that invocation's files.
func WithTempDir(parent, pattern string, fn func(dir string) error) error {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return fn(dir)
}

// EnsureDir publishes a complete cache atomically. Concurrent builders prepare
// separately and reuse the winner; readers never see a partially built cache.
func EnsureDir(dir string, prepare func(work string) error) error {
	if ok, err := cacheReady(dir); ok || err != nil {
		return err
	}
	return WithTempDir(filepath.Dir(dir), ".prepare-", func(work string) error {
		if err := prepare(work); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(work, ".ready"), nil, 0o644); err != nil {
			return err
		}
		if err := os.Rename(work, dir); err != nil {
			if ok, checkErr := cacheReady(dir); ok || checkErr != nil {
				return checkErr
			}
			return fmt.Errorf("publishing extractor cache %s: %w; remove an incomplete cache and retry", dir, err)
		}
		return nil
	})
}

func cacheReady(dir string) (bool, error) {
	info, err := os.Stat(filepath.Join(dir, ".ready"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("invalid extractor cache %s: remove it and retry", dir)
	}
	return true, nil
}
