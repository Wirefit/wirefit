// Package javatool makes the Java extractor self-bootstrapping: the
// WirefitExtract source is embedded in the wirefit-java binary, its Jackson dependencies are
// downloaded once from Maven Central with pinned SHA-256 checksums, and the
// classes are compiled on demand into the user cache. Services need zero
// build-file changes (Phase 1 amendment to PRD 1.3).
package javatool

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wirefit/wirefit/internal/extrun"
)

//go:embed WirefitExtract.java
var extractorSource string

// extractorVersion keys the compile cache; bump on WirefitExtract.java changes.
const extractorVersion = "0.3.0"

type dep struct {
	file, path, sha256 string
}

// Pinned, checksum-verified (NF5: nothing unverified runs in CI).
var deps = []dep{
	{"jackson-core-2.22.0.jar", "com/fasterxml/jackson/core/jackson-core/2.22.0/jackson-core-2.22.0.jar",
		"d2e8dd4df1e0f61b786ea06792f5bf4235d8278f158f3be6e997e955931c0c98"},
	{"jackson-databind-2.22.0.jar", "com/fasterxml/jackson/core/jackson-databind/2.22.0/jackson-databind-2.22.0.jar",
		"3520a0351f294699e3e1b7a37c7a726afd81e1a89ae702ac7d47ff347fd2ecbf"},
	{"jackson-annotations-2.22.jar", "com/fasterxml/jackson/core/jackson-annotations/2.22/jackson-annotations-2.22.jar",
		"21ddb598807d3a51a876704eb979d9296e1c6a6f47ab1826ff88c6d6a127a2d0"},
	{"jackson-datatype-jdk8-2.22.0.jar", "com/fasterxml/jackson/datatype/jackson-datatype-jdk8/2.22.0/jackson-datatype-jdk8-2.22.0.jar",
		"f1051ed0938aa5edb7567ab19c2c7e1fade58f7fbad43d99a74cb506389c1ac5"},
}

const mavenCentral = "https://repo1.maven.org/maven2/"

// Bounded client so a slow/blocked Maven Central fails fast with a clear error
// instead of hanging the first extract indefinitely (no timeout = silent hang).
var httpClient = &http.Client{Timeout: 45 * time.Second}

func cacheDir() (string, error) {
	return extrun.CacheDir("java-extractor", extractorVersion)
}

// RunOptions configures one Java extraction invocation.
type RunOptions struct {
	ProjectDir  string // service project dir (classpath resolution root)
	Classpath   string // explicit service classpath; "" → resolve via BuildTool
	BuildTool   string // auto|maven|gradle|none
	ExtractorCP string // override for WirefitExtract+jackson; "" → EnsureExtractor
	Mapper      string // ObjectMapper provider <fqn>#<method>; "" → none
	JavaBin     string // java binary (default "java")
}

// Run extracts IR for the given fully-qualified type names by invoking the
// bootstrapped WirefitExtract against the service classpath. Returns raw IR JSON
// keyed by FQN.
func Run(opts RunOptions, fqns []string) (map[string]json.RawMessage, error) {
	// Service classpath: explicit override, or interrogate the build tool —
	// zero build-file changes required in the service (PRD 1.3 amendment).
	serviceCP := opts.Classpath
	if serviceCP == "" {
		var err error
		serviceCP, err = ResolveClasspath(opts.ProjectDir, opts.BuildTool)
		if err != nil {
			return nil, err
		}
	}
	// Extractor classpath: self-bootstrapping cache (pinned, checksummed jars +
	// embedded WirefitExtract compiled on demand) unless overridden.
	extractorCP := opts.ExtractorCP
	if extractorCP == "" {
		var err error
		extractorCP, err = EnsureExtractor()
		if err != nil {
			return nil, err
		}
	}
	// Service classpath first: the service's own Jackson version wins.
	javaArgs := []string{"-cp", serviceCP + string(os.PathListSeparator) + extractorCP}
	if opts.Mapper != "" {
		javaArgs = append(javaArgs, "-Dwirefit.mapper="+opts.Mapper)
	}
	javaArgs = append(javaArgs, "io.wirefit.extract.WirefitExtract")
	javaArgs = append(javaArgs, fqns...)
	return extrun.Run("java", exec.Command(opts.JavaBin, javaArgs...))
}

// EnsureExtractor returns the classpath (WirefitExtract classes + Jackson jars)
// needed to run the extractor, bootstrapping the cache if necessary.
func EnsureExtractor() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	var parts []string

	classes := filepath.Join(dir, "classes")
	parts = append(parts, classes)

	var jarPaths []string
	for _, d := range deps {
		p := filepath.Join(dir, d.file)
		if err := ensureJar(p, d); err != nil {
			return "", err
		}
		jarPaths = append(jarPaths, p)
	}
	parts = append(parts, jarPaths...)

	marker := filepath.Join(classes, "io", "wirefit", "extract", "WirefitExtract.class")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		if err := compile(dir, classes, jarPaths); err != nil {
			return "", err
		}
	}
	return strings.Join(parts, string(os.PathListSeparator)), nil
}

func ensureJar(path string, d dep) error {
	if ok, _ := verify(path, d.sha256); ok {
		return nil
	}
	resp, err := httpClient.Get(mavenCentral + d.path)
	if err != nil {
		return fmt.Errorf("download %s from Maven Central: %w\n  (set a reachable proxy via HTTPS_PROXY, or pre-place the jar in %s)", d.file, err, filepath.Dir(path))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", d.file, resp.StatusCode)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if ok, sum := verify(tmp, d.sha256); !ok {
		os.Remove(tmp)
		return fmt.Errorf("checksum mismatch for %s: got %s; refusing to use it", d.file, sum)
	}
	return os.Rename(tmp, path)
}

func verify(path, want string) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want, got
}

func compile(dir, classes string, jarPaths []string) error {
	javac, err := findJavac()
	if err != nil {
		return err
	}
	src := filepath.Join(dir, "WirefitExtract.java")
	if err := os.WriteFile(src, []byte(extractorSource), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(javac, "--release", "11",
		"-cp", strings.Join(jarPaths, string(os.PathListSeparator)),
		"-d", classes, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compiling extractor with %s failed: %s: %w", javac, out, err)
	}
	return nil
}

func findJavac() (string, error) {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		p := filepath.Join(home, "bin", "javac")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("javac"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("javac not found (JAVA_HOME or PATH): a JDK is required to extract Java DTOs")
}
