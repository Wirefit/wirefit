package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wirefit/wirefit/internal/store"
)

func TestRecordDeploySources(t *testing.T) {
	for _, mode := range []string{"published", "ir", "promotion", "promotion-manifest"} {
		t.Run(mode, func(t *testing.T) {
			st := promoRepo(t)
			mf := filepath.Join(st.Dir, "contracts/web-app/manifest.yaml")
			a, b := blob(t, st, irA), blob(t, st, irB)
			source := &store.ServiceLock{RecordedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), RecordedBy: "old", Provides: map[string]string{"retired.get": b}, Consumes: map[string]string{"order-service/orders.get": b}}
			saveLock(t, st, "dev", store.EnvLock{"web-app": source})
			other := &store.ServiceLock{Provides: map[string]string{"orders.get": a}}
			saveLock(t, st, "stg", store.EnvLock{"order-service": other})
			args := []string{"--contracts-repo", st.Dir, "--env", "stg", "--no-commit"}
			want := map[string]string{"order-service/orders.get": a, "billing/invoices.get": a, "phantom/things.list": a, "ghost/stuff.get": a}
			var provides map[string]string
			switch mode {
			case "published":
				args = append(args, "-f", mf)
			case "ir":
				root := t.TempDir()
				for key := range want {
					writeRepoFile(t, root, "consumes/"+key+".ir.json", irB)
					want[key] = b
				}
				args = append(args, "-f", mf, "--ir", root)
			case "promotion", "promotion-manifest":
				args = append(args, "--from-env", "dev")
				if mode == "promotion" {
					args = append(args, "--service", "web-app", "-f", "does-not-exist.yaml")
				} else {
					args = append(args, "-f", mf)
				}
				want = source.Consumes
				provides = source.Provides
			}
			t.Setenv("WIREFIT_DEPLOYER", "new")
			if code := cmdRecordDeploy(args); code != 0 {
				t.Fatalf("exit = %d", code)
			}
			lock, err := st.LoadEnvLock("stg")
			if err != nil {
				t.Fatal(err)
			}
			got := lock["web-app"]
			if !reflect.DeepEqual(got.Consumes, want) {
				t.Errorf("consumes = %v, want %v", got.Consumes, want)
			}
			if len(got.Provides) != len(provides) {
				t.Errorf("provides = %v, want %v", got.Provides, provides)
			}
			for key, hash := range provides {
				if got.Provides[key] != hash {
					t.Errorf("%s = %s, want %s", key, got.Provides[key], hash)
				}
			}
			for _, hash := range got.Consumes {
				if _, err := st.ReadBlob(hash); err != nil {
					t.Fatal(err)
				}
			}
			if got.RecordedBy != "new" || !got.RecordedAt.After(source.RecordedAt) {
				t.Errorf("metadata = %+v", got)
			}
			if !reflect.DeepEqual(lock["order-service"], other) {
				t.Error("unrelated deployment changed")
			}
			dev, err := st.LoadEnvLock("dev")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dev["web-app"], source) {
				t.Error("source deployment changed")
			}
		})
	}
}

func TestRecordDeployIRIncludesProvider(t *testing.T) {
	st := promoRepo(t)
	root := t.TempDir()
	writeRepoFile(t, root, "provides/orders.get.ir.json", irB)
	args := []string{"--contracts-repo", st.Dir, "--env", "dev", "--no-commit", "-f", filepath.Join(st.Dir, "contracts/order-service/manifest.yaml"), "--ir", root}
	if code := cmdRecordDeploy(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	lock, err := st.LoadEnvLock("dev")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lock["order-service"].Provides["orders.get"], blob(t, st, irB); got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}

func TestRecordDeployFailureLeavesTarget(t *testing.T) {
	for _, mode := range []string{"conflict", "empty-ir", "service-only", "bad-service", "bad-source", "same-env", "missing-service", "missing-blob", "invalid-ir", "missing-ir"} {
		t.Run(mode, func(t *testing.T) {
			st := promoRepo(t)
			h := blob(t, st, irA)
			saveLock(t, st, "stg", store.EnvLock{"web-app": {Consumes: map[string]string{"order-service/orders.get": h}}})
			saveLock(t, st, "dev", store.EnvLock{"web-app": {Consumes: map[string]string{"order-service/orders.get": h}}})
			path := filepath.Join(st.Dir, "_envs/stg.lock.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--contracts-repo", st.Dir, "--env", "stg", "--no-commit"}
			switch mode {
			case "conflict":
				args = append(args, "--ir", "candidate", "--from-env", "dev")
			case "empty-ir":
				args = append(args, "--ir", "")
			case "service-only":
				args = append(args, "--service", "web-app")
			case "bad-service":
				args = append(args, "--from-env", "dev", "--service", "../web-app")
			case "bad-source":
				args = append(args, "--from-env", "../dev")
			case "same-env":
				args = append(args, "--from-env", "stg")
			case "missing-service":
				args = append(args, "--from-env", "dev", "--service", "absent")
			case "missing-blob":
				if err := os.Remove(filepath.Join(st.Dir, "_blobs", h[len("sha256:"):]+".ir.json")); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--from-env", "dev", "--service", "web-app")
			case "invalid-ir", "missing-ir":
				root := t.TempDir()
				if mode == "invalid-ir" {
					writeRepoFile(t, root, "provides/orders.get.ir.json", `{"type":"object","unknown":true}`)
				}
				args = append(args, "--ir", root, "-f", filepath.Join(st.Dir, "contracts/order-service/manifest.yaml"))
			}
			if code := cmdRecordDeploy(args); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Error("failed recording changed target lock")
			}
		})
	}
}

func TestRecordDeployIRNormalizesBeforeHashing(t *testing.T) {
	st := promoRepo(t)
	root := t.TempDir()
	writeRepoFile(t, root, "provides/orders.get.ir.json", strings.Replace(irAB, `["a","b"]`, `["b","a"]`, 1))
	args := []string{"--contracts-repo", st.Dir, "--env", "dev", "--no-commit", "-f", filepath.Join(st.Dir, "contracts/order-service/manifest.yaml"), "--ir", root}
	if code := cmdRecordDeploy(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	lock, err := st.LoadEnvLock("dev")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lock["order-service"].Provides["orders.get"], blob(t, st, irAB); got != want {
		t.Fatalf("hash = %s, want normalized %s", got, want)
	}
}
