// Package pytool runs the official Python extractor. The WireFit-owned
// extract.py source is embedded in wirefit-py, while Python, Pydantic,
// and service imports come from the target service environment.
package pytool

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wirefit/wirefit/internal/extrun"
)

//go:embed extract.py
var extractorSource string

// extractorVersion keys the cache; bump on extract.py changes.
const extractorVersion = "0.1.0"

func cacheDir() (string, error) {
	return extrun.CacheDir("py-extractor", extractorVersion)
}

// RunOptions configures one Python extraction invocation.
type RunOptions struct {
	ProjectDir string
	PythonBin  string
}

// EnsureExtractor returns the path to the embedded Python extractor script in
// the user cache.
func EnsureExtractor() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	script := filepath.Join(dir, "extract.py")
	if err := os.WriteFile(script, []byte(extractorSource), 0o644); err != nil {
		return "", err
	}
	return script, nil
}

// Run extracts IR for Python specs by invoking the embedded extractor with the
// service's Python environment.
func Run(opts RunOptions, provided, consumed []string) (map[string]json.RawMessage, error) {
	python, err := pythonPath(opts.ProjectDir, opts.PythonBin)
	if err != nil {
		return nil, err
	}
	if err := checkPydantic(python); err != nil {
		return nil, err
	}
	script, err := EnsureExtractor()
	if err != nil {
		return nil, err
	}
	return extrun.Run("python", exec.Command(python, args(script, opts.ProjectDir, provided, consumed)...))
}

func pythonPath(projectDir, bin string) (string, error) {
	if bin == "" {
		bin = "python3"
	}
	if !filepath.IsAbs(bin) && strings.ContainsAny(bin, `/\`) {
		if projectDir == "" {
			projectDir = "."
		}
		bin = filepath.Join(projectDir, bin)
	}
	python, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("python not found: Python 3 with pydantic v2 is required to extract Python DTOs")
	}
	return python, nil
}

func checkPydantic(python string) error {
	code := `import pydantic
version = getattr(pydantic, "__version__", getattr(pydantic, "VERSION", "0"))
major = int(version.split(".", 1)[0])
from pydantic import TypeAdapter
raise SystemExit(0 if major >= 2 else 1)
`
	if err := exec.Command(python, "-c", code).Run(); err != nil {
		return fmt.Errorf("pydantic v2 is required; run wirefit-py --python pointing at your service environment")
	}
	return nil
}

func args(script, projectDir string, provided, consumed []string) []string {
	out := []string{script, "--project", projectDir}
	for _, s := range provided {
		out = append(out, "out="+s)
	}
	for _, s := range consumed {
		out = append(out, "in="+s)
	}
	return out
}
