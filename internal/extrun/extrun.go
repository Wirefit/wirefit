// Package extrun holds the subprocess + cache plumbing shared by the built-in
// extractors (gotool, javatool, tstool). It does not speak the extproto wire
// protocol — the built-ins pass specs as CLI args and emit a bare IR map.
package extrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const DefaultTimeout = 5 * time.Minute

// Run executes a built-in extractor subprocess that emits a JSON object mapping
// each requested spec to its IR document, and returns it. build constructs the
// command from a context carrying DefaultTimeout, so the process is killed if it
// overruns; the caller sets cmd.Dir / cmd.Stdin on the returned command. stderr
// is forwarded so the extractor's own diagnostics reach the user; name (e.g.
// "go", "ts", "java") labels the error messages.
func Run(name string, build func(ctx context.Context) *exec.Cmd) (map[string]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	cmd := build(ctx)
	cmd.Stderr = os.Stderr
	// A killed `go run` can leave its compiled child holding the stdout pipe;
	// WaitDelay bounds how long Wait then blocks before forcing it closed.
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s extractor timed out after %s", name, DefaultTimeout)
	}
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
