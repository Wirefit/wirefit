package pytool

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureExtractorWritesScriptToCache(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	got, err := EnsureExtractor()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, "wirefit", "py-extractor", extractorVersion, "extract.py")
	if got != want {
		t.Fatalf("script = %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "wirefit Python extractor") {
		t.Fatalf("cached script does not look like the embedded extractor")
	}
}

func TestArgs(t *testing.T) {
	got := args("extract.py", "/svc", []string{"out.py#Order"}, []string{"in.py#Order"})
	want := []string{"extract.py", "--project", "/svc", "out=out.py#Order", "in=in.py#Order"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestRunMissingPython(t *testing.T) {
	_, err := Run(RunOptions{ProjectDir: ".", PythonBin: filepath.Join(t.TempDir(), "missing-python")}, nil, nil)
	if err == nil || err.Error() != "python not found: Python 3 with pydantic v2 is required to extract Python DTOs" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunMissingPydantic(t *testing.T) {
	python := fakePython(t, `#!/bin/sh
if [ "$1" = "-c" ]; then
  exit 1
fi
printf '{}'
`)
	_, err := Run(RunOptions{ProjectDir: ".", PythonBin: python}, nil, nil)
	if err == nil || err.Error() != "pydantic v2 is required; run wirefit-py --python pointing at your service environment" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunUsesPythonCommand(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	log := filepath.Join(t.TempDir(), "args.txt")
	python := fakePython(t, `#!/bin/sh
if [ "$1" = "-c" ]; then
  exit 0
fi
printf '%s\n' "$@" > `+shellQuote(log)+`
printf '{"ok":{"type":"string"}}'
`)
	got, err := Run(RunOptions{ProjectDir: "/svc", PythonBin: python}, []string{"a.py#Out"}, []string{"b.py#In"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ok"]; !ok {
		t.Fatalf("expected fake schema in %v", got)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, "wirefit", "py-extractor", extractorVersion, "extract.py") + "\n--project\n/svc\nout=a.py#Out\nin=b.py#In\n"
	if string(data) != want {
		t.Fatalf("fake python args:\n%s\nwant:\n%s", data, want)
	}
}

func TestRunResolvesRelativePythonFromProjectDir(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	root := t.TempDir()
	project := filepath.Join(root, "service")
	venv := filepath.Join(project, ".venv", "bin")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "args.txt")
	python := filepath.Join(venv, "python")
	writeFakePython(t, python, `#!/bin/sh
if [ "$1" = "-c" ]; then
  exit 0
fi
printf '%s\n' "$@" > `+shellQuote(log)+`
printf '{"ok":{"type":"string"}}'
`)
	t.Chdir(t.TempDir())

	got, err := Run(RunOptions{ProjectDir: project, PythonBin: ".venv/bin/python"}, []string{"a.py#Out"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ok"]; !ok {
		t.Fatalf("expected fake schema in %v", got)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, "wirefit", "py-extractor", extractorVersion, "extract.py") + "\n--project\n" + project + "\nout=a.py#Out\n"
	if string(data) != want {
		t.Fatalf("fake python args:\n%s\nwant:\n%s", data, want)
	}
}

func fakePython(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake python is unix-only")
	}
	path := filepath.Join(t.TempDir(), "python")
	writeFakePython(t, path, body)
	return path
}

func writeFakePython(t *testing.T, path, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake python is unix-only")
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
