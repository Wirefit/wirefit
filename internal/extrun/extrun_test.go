package extrun

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache root")
	old := UserCacheDir
	UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { UserCacheDir = old })

	for _, tc := range []struct{ name, version string }{
		{"py-extractor", "0.1.0"},
		{"java-extractor", "0.1.0"},
		{"py-extractor", "0.2.0"},
	} {
		t.Run(tc.name+"/"+tc.version, func(t *testing.T) {
			got, err := CacheDir(tc.name, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, "wirefit", tc.name, tc.version)
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
			marker := filepath.Join(got, "artifact")
			if err := os.WriteFile(marker, []byte("cached"), 0o644); err != nil {
				t.Fatal(err)
			}
			if again, err := CacheDir(tc.name, tc.version); err != nil || again != got {
				t.Fatalf("repeat cache = %q, %v; want %q, nil", again, err, got)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "cached" {
				t.Fatalf("cached artifact = %q, %v", data, err)
			}
		})
	}
}

func TestCacheDirResolverError(t *testing.T) {
	want := errors.New("cache root unavailable")
	old := UserCacheDir
	UserCacheDir = func() (string, error) { return "", want }
	t.Cleanup(func() { UserCacheDir = old })

	if got, err := CacheDir("py-extractor", "0.1.0"); got != "" || !errors.Is(err, want) {
		t.Fatalf("cache = %q, %v; want empty path and resolver error", got, err)
	}
}

func TestCacheDirCreationError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "wirefit"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := UserCacheDir
	UserCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { UserCacheDir = old })

	got, err := CacheDir("py-extractor", "0.1.0")
	var pathErr *os.PathError
	if got != "" || !errors.As(err, &pathErr) {
		t.Fatalf("cache = %q, %v; want empty path and filesystem error", got, err)
	}
}

func TestRunRejectsNonJSONOutput(t *testing.T) {
	_, err := Run("go", exec.Command("printf", "oops"))
	if err == nil {
		t.Fatal("expected an error for non-JSON extractor output")
	}
	if !strings.Contains(err.Error(), "bad go extractor output") {
		t.Fatalf("expected the unmarshal guard to fire, got: %v", err)
	}
}

func TestRunReturnsSchemaMap(t *testing.T) {
	out, err := Run("go", exec.Command("printf", `{"x#T":{"type":"string"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out["x#T"]; !ok {
		t.Fatalf("expected key x#T in %v", out)
	}
}

func TestCachePathSeparatesInputs(t *testing.T) {
	root := t.TempDir()
	a := CachePath(root, "ab", "c")
	for _, inputs := range [][]string{{"a", "bc"}, {"ab", "d"}, {"ab", "c", ""}} {
		if b := CachePath(root, inputs...); b == a {
			t.Fatalf("different inputs share cache %q", a)
		}
	}
	if CachePath(root, "ab", "c") != a {
		t.Fatal("cache path is not stable")
	}
}

func TestWithTempDirCleansOnlyItsOwnFiles(t *testing.T) {
	root := t.TempDir()
	sibling := filepath.Join(root, "other-run")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	want := errors.New("prepare failed")
	var work string
	err := WithTempDir(root, "extract-", func(dir string) error {
		work = dir
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("temporary directory survived: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling directory was removed: %v", err)
	}
}

func TestEnsureDirFailureCanBeRetried(t *testing.T) {
	root := t.TempDir()
	dir := CachePath(root, "source")
	want := errors.New("prepare failed")
	err := EnsureDir(dir, func(work string) error {
		if err := os.WriteFile(filepath.Join(work, "artifact"), []byte("partial"), 0o644); err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed preparation left files: %v, %v", entries, err)
	}
	if err := EnsureDir(dir, func(work string) error {
		return os.WriteFile(filepath.Join(work, "artifact"), []byte("complete"), 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(dir, func(string) error { return want }); err != nil {
		t.Fatalf("completed cache rebuilt: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "artifact")); err != nil || string(data) != "complete" {
		t.Fatalf("artifact = %q, %v", data, err)
	}
}

func TestEnsureDirConcurrentBuildersPublishOneCompleteCache(t *testing.T) {
	dir := CachePath(t.TempDir(), "source")
	started := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- EnsureDir(dir, func(work string) error {
				if err := os.WriteFile(filepath.Join(work, "first"), []byte(work), 0o644); err != nil {
					return err
				}
				started <- work
				<-release
				return os.WriteFile(filepath.Join(work, "second"), []byte(work), 0o644)
			})
		}()
	}
	a, b := <-started, <-started
	_, statErr := os.Stat(dir)
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if a == b || !os.IsNotExist(statErr) {
		t.Fatalf("preparation shared a directory or exposed partial output: %q, %q, %v", a, b, statErr)
	}
	first, err := os.ReadFile(filepath.Join(dir, "first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "second"))
	if err != nil || string(first) != string(second) {
		t.Fatalf("mixed artifacts: %q, %q, %v", first, second, err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil || len(entries) != 1 {
		t.Fatalf("preparation directories survived: %v, %v", entries, err)
	}
}
