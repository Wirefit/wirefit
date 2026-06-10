package main

// Markdown report renderer (PRD 1.10 / 3.6): one body usable as a GitHub PR
// comment or GitLab MR note. CI adapters stay thin — they post this verbatim.

import (
	"fmt"
	"strings"

	"github.com/wirefit/wirefit/internal/diff"
)

const maxReportBytes = 65000 // GitHub comment limit, with headroom (PRD 1.10)

var mdBadges = map[diff.Class]string{
	diff.Breaking: "🔴 breaking",
	diff.Warning:  "⚠️ warning",
	diff.Safe:     "🟢 safe",
	diff.Neutral:  "⚪ neutral",
}

func renderMarkdown(results map[string]*diff.Result, worst int) []byte {
	var b strings.Builder
	b.WriteString("### wirefit contract check\n\n")
	if worst == 0 {
		b.WriteString("**Result: compatible** — no breaking changes.\n")
	} else {
		b.WriteString("**Result: 🔴 BREAKING** — do not merge.\n")
	}
	for _, key := range sortedResultKeys(results) {
		r := results[key]
		b.WriteString("\n<details open><summary><b>" + key + "</b>")
		if r.ColdStart {
			b.WriteString(" <i>(cold start: no consumers registered — not enforced)</i>")
		}
		b.WriteString("</summary>\n\n")
		if len(r.Findings) == 0 {
			b.WriteString("no contract-relevant changes\n")
		} else {
			b.WriteString("| | rule | path | detail |\n|---|---|---|---|\n")
			for _, f := range r.Findings {
				detail := f.Message
				if len(f.ConsumedBy) > 0 {
					detail += fmt.Sprintf(" — consumed by: %s", strings.Join(f.ConsumedBy, ", "))
				}
				detail = strings.ReplaceAll(detail, "|", "\\|")
				fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s |\n", mdBadges[f.Class], f.Rule, f.Path, detail)
			}
		}
		b.WriteString("\n</details>\n")
	}
	out := b.String()
	if len(out) > maxReportBytes {
		out = out[:maxReportBytes-60] + "\n\n… *(truncated — see CI log for the full report)*\n"
	}
	return []byte(out)
}
