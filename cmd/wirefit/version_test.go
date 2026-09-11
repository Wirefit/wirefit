package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVersionBuild(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ldflags string
		want    string
	}{
		{name: "development", want: "wirefit 0.3.0-dev\n"},
		{
			name:    "release",
			ldflags: "-s -w -X main.version=0.0.0-version-test",
			want:    "wirefit 0.0.0-version-test\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := "wirefit"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			binary := filepath.Join(t.TempDir(), name)
			cmd := exec.Command("go", "build", "-ldflags", tt.ldflags, "-o", binary, ".")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, out)
			}
			out, err := exec.Command(binary, "version").CombinedOutput()
			if err != nil {
				t.Fatalf("version: %v\n%s", err, out)
			}
			if string(out) != tt.want {
				t.Errorf("version output = %q, want %q", out, tt.want)
			}
		})
	}
}
