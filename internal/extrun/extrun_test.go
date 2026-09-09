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
