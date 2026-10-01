// Package tstool makes the TypeScript extractor self-bootstrapping: the
// extract.js source is embedded in the wirefit-ts binary and its pinned
// typescript dependency is npm-installed once into the user cache
// (Phase 2, PRD 2.1).
package tstool

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wirefit/wirefit/internal/extrun"
)

//go:embed extract.js
var extractorSource string

// extractorVersion namespaces the cache; source edits get their own artifacts.
const extractorVersion = "0.3.0"

// typescriptVersion is the pinned compiler dependency. npm verifies its
// integrity from the lockfile-equivalent metadata at install time.
const typescriptVersion = "6.0.3"

func cacheDir() (string, error) {
	return extrun.CacheDir("ts-extractor", extractorVersion)
}

// EnsureExtractor returns the path to a ready-to-run extract.js with its
// typescript dependency installed alongside.
func EnsureExtractor() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	source := extractorSource
	pkg := fmt.Sprintf(`{"name":"wirefit-ts-extractor","private":true,"dependencies":{"typescript":"%s"}}`, typescriptVersion)
	dir = extrun.CachePath(dir, source, pkg)
	if err := extrun.EnsureDir(dir, func(work string) error {
		if err := os.WriteFile(filepath.Join(work, "extract.js"), []byte(source), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(work, "package.json"), []byte(pkg), 0o644); err != nil {
			return err
		}
		npm, err := exec.LookPath("npm")
		if err != nil {
			return fmt.Errorf("npm not found: Node.js is required to extract TypeScript DTOs")
		}
		cmd := exec.Command(npm, "install", "--no-audit", "--no-fund", "--silent")
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("installing typescript@%s failed: %s: %w", typescriptVersion, out, err)
		}
		_, err = os.Stat(filepath.Join(work, "node_modules", "typescript", "package.json"))
		return err
	}); err != nil {
		return "", err
	}
	return filepath.Join(dir, "extract.js"), nil
}

// Run executes the extractor. Specs ("file.ts#Export") are passed with their
// manifest role: provided (provider side, zod io=output) or consumed
// (consumer side, zod io=input). The role only affects Zod schemas — for
// plain types it is irrelevant. Returns raw IR JSON keyed by bare spec.
func Run(projectDir string, provided, consumed []string) (map[string]json.RawMessage, error) {
	script, err := EnsureExtractor()
	if err != nil {
		return nil, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node not found: Node.js is required to extract TypeScript DTOs")
	}
	// --experimental-strip-types: required for runtime-importing .ts schema
	// modules on the Zod path (no-op where stripping is already default).
	args := []string{
		"--experimental-strip-types", "--disable-warning=ExperimentalWarning",
		script, "--project", projectDir,
	}
	for _, s := range provided {
		args = append(args, "out="+s)
	}
	for _, s := range consumed {
		args = append(args, "in="+s)
	}
	return extrun.Run("ts", exec.Command(node, args...))
}
