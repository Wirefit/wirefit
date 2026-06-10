package main

// wirefit override add (Phase 3 P1): generates a valid override entry instead
// of making developers hand-write YAML against the schema. When the last
// `wirefit check` left exactly one breaking finding, all coordinates are
// auto-filled — the common "I know, that's the plan, let me merge" flow.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
)

const lastCheckFile = ".wirefit/last-check.json"

func cmdOverrideAdd(args []string) int {
	fs := flag.NewFlagSet("override add", flag.ContinueOnError)
	file := fs.String("f", "wirefit-overrides.yaml", "overrides file to append to")
	interaction := fs.String("interaction", "", "interaction id (auto-filled from last check when unambiguous)")
	pathFlag := fs.String("path", "", "finding path, e.g. $.customer_email")
	rule := fs.String("rule", "", "finding rule, e.g. field-removed")
	downgrade := fs.String("downgrade-to", "warning", "warning|safe")
	justification := fs.String("justification", "", "REQUIRED: why this is acceptable (reference your ticket)")
	days := fs.Int("days", 30, "validity in days (max 180)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *justification == "" {
		fmt.Fprintln(os.Stderr, "wirefit override add: --justification is required — overrides without a recorded reason defeat the audit trail")
		return 2
	}
	if *days < 1 || *days > 180 {
		fmt.Fprintln(os.Stderr, "wirefit override add: --days must be 1..180 (overrides are temporary by design)")
		return 2
	}

	// Auto-fill from the last check when coordinates are not fully specified.
	if *interaction == "" || *pathFlag == "" || *rule == "" {
		data, err := os.ReadFile(lastCheckFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefit override add: specify --interaction, --path and --rule (no recent check found to auto-fill from)")
			return 2
		}
		var results map[string]*diff.Result
		if err := json.Unmarshal(data, &results); err != nil {
			fmt.Fprintln(os.Stderr, "wirefit override add:", err)
			return 2
		}
		type cand struct {
			interaction string
			f           diff.Finding
		}
		var cands []cand
		for key, r := range results {
			var id string
			if _, err := fmt.Sscanf(key, "provides %s", &id); err != nil {
				if n, _ := fmt.Sscanf(key, "consumes %s", &id); n == 1 {
					if i := lastSlash(id); i >= 0 {
						id = id[i+1:]
					}
				}
			}
			if id == "" {
				continue
			}
			for _, f := range r.Findings {
				if f.Class == diff.Breaking {
					cands = append(cands, cand{id, f})
				}
			}
		}
		if len(cands) != 1 {
			fmt.Fprintf(os.Stderr, "wirefit override add: %d breaking finding(s) in the last check — specify --interaction/--path/--rule explicitly:\n", len(cands))
			for _, c := range cands {
				fmt.Fprintf(os.Stderr, "  --interaction %s --path '%s' --rule %s\n", c.interaction, c.f.Path, c.f.Rule)
			}
			return 2
		}
		*interaction, *pathFlag, *rule = cands[0].interaction, cands[0].f.Path, cands[0].f.Rule
	}

	expires := time.Now().AddDate(0, 0, *days).Format("2006-01-02")
	entry := fmt.Sprintf(
		"  - interaction: %s\n    path: %q\n    rule: %s\n    downgrade-to: %s\n    justification: %q\n    expires: %q\n",
		*interaction, *pathFlag, *rule, *downgrade, *justification, expires)

	existing, err := os.ReadFile(*file)
	if os.IsNotExist(err) {
		existing = []byte("overrides:\n")
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "wirefit override add:", err)
		return 2
	}
	if err := os.WriteFile(*file, append(existing, []byte(entry)...), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "wirefit override add:", err)
		return 2
	}
	fmt.Printf("added override to %s: %s %s %s → %s (expires %s)\n",
		*file, *interaction, *pathFlag, *rule, *downgrade, expires)
	fmt.Println("re-run `wirefit check` to confirm it applies")
	return 0
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
