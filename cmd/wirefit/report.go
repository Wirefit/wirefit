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
		b.WriteString("**Result: compatible**, no breaking changes.\n")
	} else {
		b.WriteString("**Result: 🔴 BREAKING**, do not merge.\n")
	}
	for _, key := range sortedResultKeys(results) {
		r := results[key]
		b.WriteString("\n<details open><summary><b>" + key + "</b>")
		if r.ColdStart {
			b.WriteString(" <i>(cold start: no consumers registered, not enforced)</i>")
		}
		b.WriteString("</summary>\n\n")
		if len(r.Findings) == 0 {
			b.WriteString("no contract-relevant changes\n")
		} else {
			b.WriteString("| | rule | path | detail |\n|---|---|---|---|\n")
			for _, f := range r.Findings {
				detail := f.Message
				if len(f.ConsumedBy) > 0 {
					detail += fmt.Sprintf("; consumed by: %s", strings.Join(f.ConsumedBy, ", "))
				}
				detail = strings.ReplaceAll(detail, "|", "\\|")
				fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s |\n", mdBadges[f.Class], f.Rule, f.Path, detail)
			}
		}
		b.WriteString("\n</details>\n")
	}
	out := b.String()
	if len(out) > maxReportBytes {
		out = out[:maxReportBytes-60] + "\n\n… *(truncated; see CI log for the full report)*\n"
	}
	return []byte(out)
}

func matrixMDBadge(status matrixStatus) string {
	switch status {
	case matrixStatusOK:
		return "✅"
	case matrixStatusWarning:
		return "⚠️"
	case matrixStatusIncompatible:
		return "🔴"
	case matrixStatusUntracked:
		return "⚪"
	case matrixStatusError:
		return "❗"
	}
	return ""
}

// recLabel is the markdown version cell: empty for a missing record, otherwise
// the publish counter (or hash fallback). Hashes stay an HTML/JSON detail.
func recLabel(r *deployRecord) string {
	if r == nil {
		return ""
	}
	return r.Label()
}

// renderMatrixMD renders the deployed matrix as a standalone markdown page
// (`matrix --format md` / `-o *.md`).
func renderMatrixMD(edges []matrixEdge, promos []promoEdge) []byte {
	var b strings.Builder
	b.WriteString("# wirefit deployed compatibility matrix\n\n")
	b.WriteString("| env | consumer | version | provider / interaction | version | status | detail |\n|---|---|---|---|---|---|---|\n")
	for _, e := range edges {
		detail := strings.ReplaceAll(e.Detail, "|", "\\|")
		fmt.Fprintf(&b, "| %s | %s | %s | %s / %s | %s | %s %s | %s |\n",
			e.Env, e.Consumer, recLabel(e.ConsumerRecord), e.Provider, e.Interaction,
			recLabel(e.ProviderRecord), matrixMDBadge(e.Status), e.Status, detail)
	}
	if len(promos) > 0 {
		b.WriteString("\n## promotion readiness\n\n")
		b.WriteString("| from → to | service | check | status | detail |\n|---|---|---|---|---|\n")
		for _, p := range promos {
			detail := strings.ReplaceAll(p.Detail, "|", "\\|")
			fmt.Fprintf(&b, "| %s → %s | %s | %s | %s %s | %s |\n",
				p.From, p.To, p.Service, promoCheck(p), matrixMDBadge(p.Status), p.Status, detail)
		}
	}
	return []byte(b.String())
}
