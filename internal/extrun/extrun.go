// Package extrun holds the subprocess + cache plumbing shared by the built-in
// extractors (gotool, javatool, tstool). It does not speak the extproto wire
// protocol — the built-ins pass specs as CLI args and emit a bare IR map.
package extrun

import (
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

// CacheDir returns <UserCacheDir>/wirefit/<name>/<version>, created. name is the
// per-extractor cache namespace (e.g. "java-extractor"); version keys the cache
// so a bump invalidates stale compiled/installed artifacts.
func CacheDir(name, version string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "wirefit", name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
