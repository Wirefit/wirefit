// Package javatool makes the Java extractor self-bootstrapping: the
// WirefitExtract source is embedded in the wirefit binary, its Jackson dependencies are
// downloaded once from Maven Central with pinned SHA-256 checksums, and the
// classes are compiled on demand into the user cache. Services need zero
// build-file changes (Phase 1 amendment to PRD 1.3).
package javatool

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed WirefitExtract.java
var extractorSource string

// extractorVersion keys the compile cache; bump on WirefitExtract.java changes.
const extractorVersion = "0.1.1"

type dep struct {
	file, path, sha256 string
}

// Pinned, checksum-verified (NF5: nothing unverified runs in CI).
var deps = []dep{
	{"jackson-core-2.17.2.jar", "com/fasterxml/jackson/core/jackson-core/2.17.2/jackson-core-2.17.2.jar",
		"721a189241dab0525d9e858e5cb604d3ecc0ede081e2de77d6f34fa5779a5b46"},
	{"jackson-databind-2.17.2.jar", "com/fasterxml/jackson/core/jackson-databind/2.17.2/jackson-databind-2.17.2.jar",
		"c04993f33c0f845342653784f14f38373d005280e6359db5f808701cfae73c0c"},
	{"jackson-annotations-2.17.2.jar", "com/fasterxml/jackson/core/jackson-annotations/2.17.2/jackson-annotations-2.17.2.jar",
		"873a606e23507969f9bbbea939d5e19274a88775ea5a169ba7e2d795aa5156e1"},
	{"jackson-datatype-jdk8-2.17.2.jar", "com/fasterxml/jackson/datatype/jackson-datatype-jdk8/2.17.2/jackson-datatype-jdk8-2.17.2.jar",
		"aaa98d3edabf50426bd822fad1442fbdada6e470969326cbcab5c2798f1738d9"},
}

const mavenCentral = "https://repo1.maven.org/maven2/"

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wirefit", "java-extractor", extractorVersion), nil
}

// EnsureExtractor returns the classpath (WirefitExtract classes + Jackson jars)
// needed to run the extractor, bootstrapping the cache if necessary.
func EnsureExtractor() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	resp, err := http.Get(mavenCentral + d.path)
	if err != nil {
		return fmt.Errorf("download %s: %w", d.file, err)
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
		return fmt.Errorf("checksum mismatch for %s: got %s — refusing to use it", d.file, sum)
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
	return "", fmt.Errorf("javac not found (JAVA_HOME or PATH) — a JDK is required to extract Java DTOs")
}
