package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLogBumpResolve(t *testing.T) {
	tests := []struct {
		name    string
		publish []string // hashes published in order for one ref
		want    []bool   // Bump result per publish
		resolve map[string]int
	}{
		{name: "first publish", publish: []string{"sha256:a"}, want: []bool{true},
			resolve: map[string]int{"sha256:a": 1}},
		{name: "idempotent republish", publish: []string{"sha256:a", "sha256:a"}, want: []bool{true, false},
			resolve: map[string]int{"sha256:a": 1}},
		{name: "change appends", publish: []string{"sha256:a", "sha256:b"}, want: []bool{true, true},
			resolve: map[string]int{"sha256:a": 1, "sha256:b": 2}},
		{name: "rollback is a new event", publish: []string{"sha256:a", "sha256:b", "sha256:a"},
			want:    []bool{true, true, true},
			resolve: map[string]int{"sha256:a": 3, "sha256:b": 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := VersionLog{}
			for i, h := range tt.publish {
				if got := v.Bump("provides/x", h); got != tt.want[i] {
					t.Errorf("Bump #%d = %v, want %v", i, got, tt.want[i])
				}
			}
			for h, want := range tt.resolve {
				if got := v.Resolve("provides/x", h); got != want {
					t.Errorf("Resolve(%q) = %d, want %d", h, got, want)
				}
			}
			if got := v.Resolve("provides/x", "sha256:unknown"); got != 0 {
				t.Errorf("Resolve(unknown hash) = %d, want 0", got)
			}
			if got := v.Resolve("provides/other", tt.publish[0]); got != 0 {
				t.Errorf("Resolve(unknown ref) = %d, want 0", got)
			}
		})
	}
}

func TestLoadVersionsMissingAndCorrupt(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.LoadVersions("svc")
	if err != nil || len(v) != 0 {
		t.Fatalf("missing log: got %v, %v; want empty, nil", v, err)
	}
	if err := os.MkdirAll(filepath.Dir(st.versionsPath("svc")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.versionsPath("svc"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadVersions("svc"); err == nil {
		t.Error("corrupt log must error, not read as empty")
	}
}

const irRawV2 = `{"type":"object","properties":{"a":{"type":"string","x-ct-scalar":"string"},"b":{"type":"integer","x-ct-scalar":"int32"}},"required":["a"]}`

func TestPublishVersionLog(t *testing.T) {
	repo := t.TempDir() // no .git: write-only mode
	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	m, src := writeManifest(t, work, `
service: svc
schema-version: 1
provides:
  - id: svc.pong
    kind: rest
    direction: response
    dto: X
consumes:
  - id: prov.ping
    provider: prov
    dto: Y
`)
	publish := func(provided, consumed string) {
		t.Helper()
		provides := map[string][]byte{"svc.pong": []byte(provided)}
		consumes := map[string]map[string][]byte{"prov": {"prov.ping": []byte(consumed)}}
		if err := st.Publish(m, src, provides, consumes, false); err != nil {
			t.Fatal(err)
		}
	}
	logBytes := func() []byte {
		t.Helper()
		data, err := os.ReadFile(st.versionsPath("svc"))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	publish(irRaw, irRaw)
	v, err := st.LoadVersions("svc")
	if err != nil {
		t.Fatal(err)
	}
	hash1 := v["provides/svc.pong"]
	if len(hash1) != 1 || len(v["consumes/prov/prov.ping"]) != 1 {
		t.Fatalf("first publish must log v1 for both refs, got %v", v)
	}

	// Idempotent republish: byte-identical log, no phantom bump.
	before := logBytes()
	publish(irRaw, irRaw)
	if got := logBytes(); string(got) != string(before) {
		t.Errorf("identical republish changed the log:\n%s\nwas:\n%s", got, before)
	}

	// Changing one side bumps only that ref.
	publish(irRawV2, irRaw)
	if v, err = st.LoadVersions("svc"); err != nil {
		t.Fatal(err)
	}
	if got := v.Resolve("provides/svc.pong", v["provides/svc.pong"][1]); got != 2 {
		t.Errorf("changed provider contract resolves to %d, want 2", got)
	}
	if got := v.Resolve("provides/svc.pong", hash1[0]); got != 1 {
		t.Errorf("previous provider contract resolves to %d, want 1", got)
	}
	if n := len(v["consumes/prov/prov.ping"]); n != 1 {
		t.Errorf("unchanged consumer ref bumped to %d entries", n)
	}

	// Determinism: saving the loaded log reproduces the bytes exactly.
	before = logBytes()
	if err := st.SaveVersions("svc", v); err != nil {
		t.Fatal(err)
	}
	if got := logBytes(); string(got) != string(before) {
		t.Errorf("re-save not byte-identical:\n%s\nwas:\n%s", got, before)
	}
}

func TestPublishVersionLogSurvivesDropAndReadd(t *testing.T) {
	repo := t.TempDir()
	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	withPong := `
service: svc
schema-version: 1
provides:
  - id: svc.pong
    kind: rest
    direction: response
    dto: X
`
	m, src := writeManifest(t, work, withPong)
	if err := st.Publish(m, src, map[string][]byte{"svc.pong": []byte(irRaw)}, nil, false); err != nil {
		t.Fatal(err)
	}

	// Drop the interaction: pruning removes the IR file but keeps the history,
	// so a later re-add continues the counter instead of restarting at v1.
	m2, src2 := writeManifest(t, work, "service: svc\nschema-version: 1\n")
	if err := st.Publish(m2, src2, map[string][]byte{}, nil, false); err != nil {
		t.Fatal(err)
	}
	v, err := st.LoadVersions("svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(v["provides/svc.pong"]) != 1 {
		t.Fatalf("dropped ref lost its history: %v", v)
	}

	m3, src3 := writeManifest(t, work, withPong)
	if err := st.Publish(m3, src3, map[string][]byte{"svc.pong": []byte(irRawV2)}, nil, false); err != nil {
		t.Fatal(err)
	}
	if v, err = st.LoadVersions("svc"); err != nil {
		t.Fatal(err)
	}
	if got := v.Resolve("provides/svc.pong", v["provides/svc.pong"][len(v["provides/svc.pong"])-1]); got != 2 {
		t.Errorf("re-added ref resolves to %d, want 2 (counter must continue)", got)
	}
}
