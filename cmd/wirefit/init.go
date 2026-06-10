package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// cmdInit scaffolds a contracts.yaml and, when a Java source tree is present,
// suggests DTO candidates as commented entries (PRD P1).
func cmdInit(args []string) int {
	fs_ := flag.NewFlagSet("init", flag.ContinueOnError)
	service := fs_.String("service", "", "service name (default: current directory name)")
	out := fs_.String("f", "contracts.yaml", "output manifest path")
	scan := fs_.String("scan", "", "source directory to scan for DTO candidates (default: src/main/java if present)")
	force := fs_.Bool("force", false, "overwrite an existing manifest")
	if fs_.Parse(args) != nil {
		return 2
	}
	if _, err := os.Stat(*out); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "wirefit init: %s already exists (use --force to overwrite)\n", *out)
		return 2
	}
	name := *service
	if name == "" {
		wd, _ := os.Getwd()
		name = sanitizeService(filepath.Base(wd))
	}
	scanDir := *scan
	if scanDir == "" {
		if _, err := os.Stat("src/main/java"); err == nil {
			scanDir = "src/main/java"
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "service: %s\nschema-version: 1\n\n", name)
	b.WriteString(`# What this service exposes. One entry per interaction; a REST request and
# response are two interactions with different directions.
provides: []
#  - id: ` + name + `.example-response       # dot-namespaced, globally unique per provider
#    kind: rest                              # rest | event | rpc
#    direction: response                     # response | request | event
#    dto: com.example.api.ExampleResponse    # serialization root type

# What this service consumes from others — your DTOs ARE your usage declaration.
consumes: []
#  - id: billing.invoice-created
#    provider: billing-service
#    dto: com.example.events.InvoiceCreated

#settings:
#  unknown-fields: ignore                    # reject if your deserializer is strict
#  java-mapper: com.example.config.Jackson#objectMapper  # custom ObjectMapper provider
`)

	if scanDir != "" {
		if cands := scanDTOs(scanDir); len(cands) > 0 {
			b.WriteString("\n# DTO candidates found under " + scanDir + " (uncomment + assign interaction ids):\n")
			for _, c := range cands {
				b.WriteString("#   " + c + "\n")
			}
		}
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "wirefit init:", err)
		return 2
	}
	fmt.Printf("wrote %s (service %s) — fill in provides/consumes, then run `wirefit extract`\n", *out, name)
	return 0
}

var dtoName = regexp.MustCompile(`(Response|Request|Dto|DTO|Event|Message|Payload)\.java$`)

func sanitizeService(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// scanDTOs lists FQNs of plausibly-contract-bearing classes by filename
// convention. Suggestions only — never silently inferred into the manifest.
func scanDTOs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !dtoName.MatchString(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		fqn := strings.TrimSuffix(filepath.ToSlash(rel), ".java")
		out = append(out, strings.ReplaceAll(fqn, "/", "."))
		return nil
	})
	sort.Strings(out)
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
