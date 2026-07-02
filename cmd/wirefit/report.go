package main

// Markdown report renderer (PRD 1.10 / 3.6): one body usable as a GitHub PR
// comment or GitLab MR note. CI adapters stay thin — they post this verbatim.

import (
	"bytes"
	"fmt"
	"html/template"
	"sort"
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

// renderMatrixMD renders the deployed matrix as a standalone markdown page
// (`matrix --format md` / `-o *.md`).
func renderMatrixMD(edges []matrixEdge) []byte {
	var b strings.Builder
	b.WriteString("# wirefit deployed compatibility matrix\n\n")
	b.WriteString("| env | consumer | provider / interaction | status | detail |\n|---|---|---|---|---|\n")
	for _, e := range edges {
		detail := strings.ReplaceAll(e.Detail, "|", "\\|")
		fmt.Fprintf(&b, "| %s | %s | %s / %s | %s %s | %s |\n",
			e.Env, e.Consumer, e.Provider, e.Interaction, matrixMDBadge(e.Status), e.Status, detail)
	}
	return []byte(b.String())
}

// matrixPage is the self-contained hostable page: inline CSS, no scripts, no
// timestamps (a page regenerated from the same repo state must not churn).
// Light/dark follows the viewer's system preference via the CSS variables.
var matrixPage = template.Must(template.New("matrix").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>wirefit deployed compatibility matrix</title>
<style>
  :root {
    --bg: #f6f8fa; --card: #ffffff; --border: #d8dee4;
    --text: #1f2328; --muted: #59636e;
    --ok-bg: #dafbe1; --ok-fg: #116329;
    --warn-bg: #fff8c5; --warn-fg: #7d4e00;
    --bad-bg: #ffebe9; --bad-fg: #a40e26;
    --dim-bg: #eaeef2; --dim-fg: #59636e;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #0d1117; --card: #161b22; --border: #30363d;
      --text: #e6edf3; --muted: #9198a1;
      --ok-bg: #122a1c; --ok-fg: #3fb950;
      --warn-bg: #2a2312; --warn-fg: #d29922;
      --bad-bg: #2c1518; --bad-fg: #f85149;
      --dim-bg: #21262d; --dim-fg: #9198a1;
    }
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--text);
         font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
  main { max-width: 64rem; margin: 0 auto; padding: 2.5rem 1.25rem 4rem; }
  h1 { font-size: 1.3rem; margin: 0; }
  .sub { color: var(--muted); font-size: .88rem; margin: .2rem 0 1rem; }
  .chips { display: flex; flex-wrap: wrap; gap: .5rem; margin-bottom: 1.25rem; }
  .chip, .badge { display: inline-flex; align-items: center; gap: .45rem;
                  border-radius: 999px; padding: .18rem .7rem;
                  font-size: .78rem; font-weight: 500; white-space: nowrap;
                  background: var(--dim-bg); color: var(--dim-fg); }
  .chip::before, .badge::before { content: ""; width: .5em; height: .5em;
                  border-radius: 50%; background: currentColor; flex: none; }
  .st-ok { background: var(--ok-bg); color: var(--ok-fg); }
  .st-warning { background: var(--warn-bg); color: var(--warn-fg); }
  .st-INCOMPATIBLE, .st-error { background: var(--bad-bg); color: var(--bad-fg); }
  .st-untracked { background: var(--dim-bg); color: var(--dim-fg); }
  .card { background: var(--card); border: 1px solid var(--border);
          border-radius: 10px; overflow-x: auto;
          box-shadow: 0 1px 3px rgba(31, 35, 40, .06); }
  table { border-collapse: collapse; width: 100%; min-width: 46rem; }
  th { text-align: left; font-size: .72rem; text-transform: uppercase;
       letter-spacing: .06em; color: var(--muted); font-weight: 600;
       padding: .7rem .95rem; border-bottom: 1px solid var(--border); }
  td { padding: .62rem .95rem; border-bottom: 1px solid var(--border);
       vertical-align: top; font-size: .9rem; }
  tbody tr:last-child td { border-bottom: none; }
  tbody tr:hover td { background: var(--bg); }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .84em; }
  td > code { white-space: nowrap; }
  .verdict { font-size: .92rem; font-weight: 500; margin: 0 0 1rem;
             display: flex; align-items: center; gap: .45rem; }
  .verdict::before { content: ""; width: .55em; height: .55em; border-radius: 50%;
                     background: currentColor; flex: none; }
  .verdict.bad { color: var(--bad-fg); }
  .verdict.warn { color: var(--warn-fg); }
  .verdict.good { color: var(--ok-fg); }
  .chips label { cursor: pointer; user-select: none; }
  .hint { color: var(--muted); font-size: .78rem; align-self: center; }
  section { margin-bottom: 1.9rem; }
  h2 { font-size: .95rem; margin: 0 0 .55rem; }
  .gsum { color: var(--muted); font-weight: 400; font-size: .8rem; margin-left: .6rem; }
  .detail { color: var(--muted); font-size: .85rem; }
  .empty { color: var(--muted); text-align: center; padding: 2.2rem 1rem; margin: 0; }
  footer { color: var(--muted); font-size: .78rem; margin-top: 1rem; }
  main > input { display: none; }
  #f-INCOMPATIBLE:not(:checked) ~ section tr.row-INCOMPATIBLE,
  #f-error:not(:checked) ~ section tr.row-error,
  #f-warning:not(:checked) ~ section tr.row-warning,
  #f-untracked:not(:checked) ~ section tr.row-untracked,
  #f-ok:not(:checked) ~ section tr.row-ok { display: none; }
  #f-INCOMPATIBLE:not(:checked) ~ .chips label[for="f-INCOMPATIBLE"],
  #f-error:not(:checked) ~ .chips label[for="f-error"],
  #f-warning:not(:checked) ~ .chips label[for="f-warning"],
  #f-untracked:not(:checked) ~ .chips label[for="f-untracked"],
  #f-ok:not(:checked) ~ .chips label[for="f-ok"] { opacity: .4; }
</style>
</head>
<body>
<main>
{{range .Counts}}<input type="checkbox" id="f-{{.Status}}" checked>
{{end}}<h1>Deployed compatibility matrix</h1>
<p class="sub">{{.EnvLabel}} · {{.EdgeLabel}}</p>
{{if .Verdict}}<p class="verdict {{.VerdictTone}}">{{.Verdict}}</p>
{{end}}{{if .Counts}}<div class="chips">{{range .Counts}}<label class="chip st-{{.Status}}" for="f-{{.Status}}">{{.N}} {{.Status}}</label>{{end}}<span class="hint">click a chip to hide or show that status</span></div>
{{end}}{{range .Groups}}<section>
<h2>{{.Env}}<span class="gsum">{{.Summary}}</span></h2>
<div class="card">
<table>
<thead><tr><th>consumer</th><th>provider / interaction</th><th>status</th><th>detail</th></tr></thead>
<tbody>
{{range .Edges}}<tr class="row-{{.Status}}"><td>{{.Consumer}}</td><td><code>{{.Provider}}/{{.Interaction}}</code></td><td><span class="badge st-{{.Status}}">{{.Status}}</span></td><td class="detail">{{.Detail}}</td></tr>
{{end}}</tbody>
</table>
</div>
</section>
{{else}}<div class="card"><p class="empty">no deploy records; run <code>wirefit record-deploy</code> in each service first</p></div>
{{end}}<footer>generated by <code>wirefit matrix</code></footer>
</main>
</body>
</html>
`))

type matrixStatusCount struct {
	Status matrixStatus
	N      int
}

type matrixEnvGroup struct {
	Env     string
	Summary string
	Edges   []matrixEdge
}

type matrixPageData struct {
	EnvLabel, EdgeLabel  string
	Verdict, VerdictTone string
	Counts               []matrixStatusCount
	Groups               []matrixEnvGroup
}

// matrixStatusOrder is worst first: the chip row, the verdict and each env
// section lead with the action items.
var matrixStatusOrder = []matrixStatus{matrixStatusIncompatible, matrixStatusError,
	matrixStatusWarning, matrixStatusUntracked, matrixStatusOK}

func matrixStatusRank(s matrixStatus) int {
	for i, o := range matrixStatusOrder {
		if o == s {
			return i
		}
	}
	return len(matrixStatusOrder)
}

func matrixCounts(edges []matrixEdge) []matrixStatusCount {
	n := map[matrixStatus]int{}
	for _, e := range edges {
		n[e.Status]++
	}
	var out []matrixStatusCount
	for _, s := range matrixStatusOrder {
		if n[s] > 0 {
			out = append(out, matrixStatusCount{s, n[s]})
		}
	}
	return out
}

// renderMatrixHTML renders the matrix as a self-contained HTML page
// (`matrix --format html` / `-o *.html`): one section per env, worst rows
// first, with the summary chips doubling as CSS-only status filters.
func renderMatrixHTML(edges []matrixEdge) []byte {
	es := append([]matrixEdge(nil), edges...) // don't reorder the caller's slice
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Env != b.Env {
			return a.Env < b.Env
		}
		if ra, rb := matrixStatusRank(a.Status), matrixStatusRank(b.Status); ra != rb {
			return ra < rb
		}
		if a.Consumer != b.Consumer {
			return a.Consumer < b.Consumer
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Interaction < b.Interaction
	})

	d := matrixPageData{Counts: matrixCounts(es), EdgeLabel: plural(len(es), "edge")}
	for start := 0; start < len(es); {
		end := start
		for end < len(es) && es[end].Env == es[start].Env {
			end++
		}
		g := matrixEnvGroup{Env: es[start].Env, Edges: es[start:end]}
		var parts []string
		for _, c := range matrixCounts(g.Edges) {
			parts = append(parts, fmt.Sprintf("%d %s", c.N, c.Status))
		}
		g.Summary = strings.Join(parts, " · ")
		d.Groups = append(d.Groups, g)
		start = end
	}
	d.EnvLabel = plural(len(d.Groups), "env")

	failing, warnings := 0, 0
	var failEnvs []string
	for _, e := range es {
		switch e.Status {
		case matrixStatusIncompatible, matrixStatusError:
			failing++
			if len(failEnvs) == 0 || failEnvs[len(failEnvs)-1] != e.Env {
				failEnvs = append(failEnvs, e.Env)
			}
		case matrixStatusWarning:
			warnings++
		}
	}
	switch {
	case failing > 0:
		d.Verdict = fmt.Sprintf("%s in %s", plural(failing, "failing edge"), strings.Join(failEnvs, ", "))
		d.VerdictTone = "bad"
	case warnings > 0:
		d.Verdict = fmt.Sprintf("no failing edges · %s", plural(warnings, "warning"))
		d.VerdictTone = "warn"
	case len(es) > 0:
		d.Verdict = "all edges compatible"
		d.VerdictTone = "good"
	}

	var b bytes.Buffer
	if err := matrixPage.Execute(&b, d); err != nil {
		// template and data shapes are fixed at compile time
		panic(err)
	}
	return b.Bytes()
}
