package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPipeline(t *testing.T) {
	cases := []struct {
		name    string
		content string // "" = no file
		want    []string
		wantErr string
	}{
		{name: "missing file", content: "", want: nil},
		{
			name:    "valid",
			content: "schema-version: 1\nenvs: [dev, staging, prod]\n",
			want:    []string{"dev", "staging", "prod"},
		},
		{
			name:    "block list",
			content: "schema-version: 1\nenvs:\n  - dev\n  - prod\n",
			want:    []string{"dev", "prod"},
		},
		{
			name:    "unknown key rejected",
			content: "schema-version: 1\nenvs: [dev, prod]\nstages: [a]\n",
			wantErr: "stages",
		},
		{
			name:    "wrong schema-version",
			content: "schema-version: 2\nenvs: [dev, prod]\n",
			wantErr: "schema-version",
		},
		{
			name:    "single env",
			content: "schema-version: 1\nenvs: [prod]\n",
			wantErr: "at least 2",
		},
		{
			name:    "duplicate env",
			content: "schema-version: 1\nenvs: [dev, dev]\n",
			wantErr: "duplicate",
		},
		{
			name:    "bad env name",
			content: "schema-version: 1\nenvs: [dev, Prod]\n",
			wantErr: "must match",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			if tc.content != "" {
				p := filepath.Join(repo, "_envs", "pipeline.yaml")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			st, err := Open(repo)
			if err != nil {
				t.Fatal(err)
			}
			got, err := st.LoadPipeline()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("want %v, got %v", tc.want, got)
				}
			}
		})
	}
}

func TestPipelineFileInvisibleToEnvs(t *testing.T) {
	repo := t.TempDir()
	envsDir := filepath.Join(repo, "_envs")
	if err := os.MkdirAll(envsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"pipeline.yaml":  "schema-version: 1\nenvs: [dev, prod]\n",
		"prod.lock.json": "{}",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(envsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	envs := st.Envs()
	if len(envs) != 1 || envs[0] != "prod" {
		t.Errorf("Envs must list lockfiles only, got %v", envs)
	}
}
