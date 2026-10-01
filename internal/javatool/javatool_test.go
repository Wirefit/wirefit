package javatool

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wirefit/wirefit/internal/extrun"
)

func TestCacheDir(t *testing.T) {
	root := t.TempDir()
	old := extrun.UserCacheDir
	extrun.UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { extrun.UserCacheDir = old })

	got, err := cacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "wirefit", "java-extractor", extractorVersion)
	if got != want {
		t.Fatalf("cache = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("cache %q is not a directory", got)
	}
}

// The compiled classes must follow the embedded source, not the version
// constant: a source edit without a bump has to land in a fresh directory.
func TestClassesDirKeyedBySource(t *testing.T) {
	a := classesDir("/cache", extractorSource)
	if filepath.Dir(a) != "/cache" || !strings.HasPrefix(filepath.Base(a), "artifacts-") {
		t.Fatalf("classes dir = %q", a)
	}
	if again := classesDir("/cache", extractorSource); again != a {
		t.Fatalf("classes dir not stable: %q vs %q", a, again)
	}
	if b := classesDir("/cache", extractorSource+"// edited\n"); b == a {
		t.Fatalf("source change kept classes dir %q", a)
	}
	old := deps
	deps = append([]dep(nil), deps...)
	t.Cleanup(func() { deps = old })
	deps[0].sha256 = "different dependency"
	if b := classesDir("/cache", extractorSource); b == a {
		t.Fatalf("dependency change kept classes dir %q", a)
	}
}

// Run with no classpath and --build-tool none must fail before ever shelling out
// to java: it proves the option threading reaches ResolveClasspath and that the
// Java path is now exercisable without the CLI command.
func TestRunBuildToolNoneRequiresClasspath(t *testing.T) {
	_, err := Run(RunOptions{ProjectDir: t.TempDir(), BuildTool: "none"}, []string{"com.example.Order"})
	if err == nil {
		t.Fatal("expected an error when --build-tool none has no explicit classpath")
	}
	if !strings.Contains(err.Error(), "requires an explicit --classpath") {
		t.Fatalf("expected the classpath guard to fire, got: %v", err)
	}
}

func TestCompileConcurrentSourcesStayIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake javac is unix-only")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "jdk", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	ready, release := filepath.Join(root, "ready"), filepath.Join(root, "release")
	t.Setenv("JAVA_HOME", filepath.Dir(bin))
	t.Setenv("WIREFIT_TEST_COMPILE_READY", ready)
	t.Setenv("WIREFIT_TEST_COMPILE_RELEASE", release)
	script := `#!/bin/sh
set -eu
if [ "$(cat "$7")" = A ]; then
  touch "$WIREFIT_TEST_COMPILE_READY"
  while [ ! -f "$WIREFIT_TEST_COMPILE_RELEASE" ]; do sleep 0.01; done
fi
mkdir -p "$6/io/wirefit/extract"
cp "$7" "$6/io/wirefit/extract/WirefitExtract.class"
`
	if err := os.WriteFile(filepath.Join(bin, "javac"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a, b := classesDir(root, "A"), classesDir(root, "B")
	done := make(chan error, 1)
	go func() { done <- compile(a, "A", nil) }()
	defer func() {
		os.WriteFile(release, nil, 0o644)
		if done != nil {
			<-done
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			done = nil
			t.Fatalf("first compiler exited before the barrier: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("first compiler did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatalf("unfinished classes were published: %v", err)
	}
	if err := compile(b, "B", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := <-done
	done = nil
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dir, source string }{{a, "A"}, {b, "B"}} {
		marker := filepath.Join(tc.dir, "classes", "io", "wirefit", "extract", "WirefitExtract.class")
		if data, err := os.ReadFile(marker); err != nil || string(data) != tc.source {
			t.Fatalf("compiled source = %q, %v; want %q", data, err, tc.source)
		}
	}
	t.Setenv("JAVA_HOME", "")
	t.Setenv("PATH", "")
	if err := compile(a, "A", nil); err != nil {
		t.Fatalf("warm cache invoked javac: %v", err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestEnsureJarConcurrentDownloads(t *testing.T) {
	content := "verified jar"
	sum := sha256.Sum256([]byte(content))
	d := dep{file: "test.jar", path: "test.jar", sha256: fmt.Sprintf("%x", sum)}
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	started, release := make(chan struct{}, 2), make(chan struct{})
	httpClient = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content))}, nil
	})}
	root := t.TempDir()
	path := filepath.Join(root, d.file)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- ensureJar(path, d) }()
	}
	<-started
	<-started
	_, statErr := os.Stat(path)
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if !os.IsNotExist(statErr) {
		t.Fatalf("unfinished download was published: %v", statErr)
	}
	if ok, _ := verify(path, d.sha256); !ok {
		t.Fatal("published jar failed verification")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("download directories survived: %v, %v", entries, err)
	}
}

func TestEnsureJarRejectsChecksumMismatch(t *testing.T) {
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	httpClient = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("invalid jar"))}, nil
	})}
	root := t.TempDir()
	if err := ensureJar(filepath.Join(root, "test.jar"), dep{file: "test.jar", sha256: "wrong"}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum error, got %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid download survived: %v, %v", entries, err)
	}
}
