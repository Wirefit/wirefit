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
func renderMatrixMD(edges []matrixEdge, promos []promoEdge) []byte {
	var b strings.Builder
	b.WriteString("# wirefit deployed compatibility matrix\n\n")
	b.WriteString("| env | consumer | provider / interaction | status | detail |\n|---|---|---|---|---|\n")
	for _, e := range edges {
		detail := strings.ReplaceAll(e.Detail, "|", "\\|")
		fmt.Fprintf(&b, "| %s | %s | %s / %s | %s %s | %s |\n",
			e.Env, e.Consumer, e.Provider, e.Interaction, matrixMDBadge(e.Status), e.Status, detail)
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

// matrixPage is the self-contained hostable page: inline CSS plus one static
// inline script, no external resources, no timestamps (a page regenerated from
// the same repo state must not churn). The script carries no template actions,
// so html/template never escapes data into a JS context; all behavior reads
// DOM attributes. Without JS the noscript style shows every detail row.
// Light/dark follows the viewer's system preference via the CSS variables.
var matrixPage = template.Must(template.New("matrix").Funcs(template.FuncMap{
	"fclass": findingStatus,
	"sthelp": func(s matrixStatus) string { return matrixStatusHelp[s] },
}).Parse(`<!doctype html>
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
  .pipeline { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem;
              margin-bottom: 1.2rem; }
  .pnode { display: inline-flex; align-items: baseline; gap: .45rem;
           text-decoration: none; }
  .pnode .psum { color: var(--muted); font-size: .75rem; }
  .parrow { font-weight: 600; }
  .pa-ok { color: var(--ok-fg); }
  .pa-warning { color: var(--warn-fg); }
  .pa-INCOMPATIBLE, .pa-error { color: var(--bad-fg); }
  .pa-untracked { color: var(--dim-fg); }
  tr[data-exp] { cursor: pointer; }
  tr[data-exp] > td:first-child::before { content: "▸ "; color: var(--muted); }
  tr[data-exp].open > td:first-child::before { content: "▾ "; }
  tr.exp > td { background: var(--bg); font-size: .85rem; }
  table.findings { min-width: 0; width: 100%; }
  .findings td { border-bottom: none; padding: .2rem .7rem .2rem 0; font-size: .85rem; }
  .prov { color: var(--muted); font-size: .8rem; margin: .4rem 0 0; }
  .ver code { color: var(--muted); }
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
<p class="sub">{{.EnvLabel}} · {{.EdgeLabel}}{{if .PromoLabel}} · {{.PromoLabel}}{{end}}</p>
{{if .Pipeline}}<nav class="pipeline">{{range .Pipeline}}{{if .Anchor}}<a class="pnode" href="#{{.Anchor}}"><span class="badge st-{{.Status}}">{{.Env}}</span><span class="psum">{{.Summary}}</span></a>{{else}}<span class="pnode"><span class="badge st-{{.Status}}">{{.Env}}</span><span class="psum">{{.Summary}}</span></span>{{end}}{{with .Arrow}} <span class="parrow pa-{{.Status}}" title="{{.Title}}">→</span> {{end}}{{end}}<span class="hint">badges = health per env · arrows = safe to promote? (hover for detail)</span></nav>
{{end}}{{if .Verdict}}<p class="verdict {{.VerdictTone}}">{{.Verdict}}</p>
{{end}}{{if .Counts}}<div class="chips">{{range .Counts}}<label class="chip st-{{.Status}}" for="f-{{.Status}}" title="{{sthelp .Status}}">{{.N}} {{.Status}}</label>{{end}}<span class="hint">click a chip to hide or show that status · click a row for findings and deployed versions</span></div>
{{end}}{{range .Groups}}<section id="{{.ID}}">
<h2>{{.Env}}<span class="gsum">{{.Summary}}</span></h2>
<div class="card">
<table>
<thead><tr><th>consumer</th><th>version</th><th>provider / interaction</th><th>version</th><th>status</th><th>detail</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="row-{{.Status}}"{{if .Expand}} data-exp{{end}}><td>{{.Consumer}}</td><td class="ver">{{with .ConsumerRecord}}<code title="recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Hash}}</code>{{end}}</td><td><code>{{.Provider}}/{{.Interaction}}</code></td><td class="ver">{{with .ProviderRecord}}<code title="recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Hash}}</code>{{end}}</td><td><span class="badge st-{{.Status}}" title="{{sthelp .Status}}">{{.Status}}</span></td><td class="detail">{{.Detail}}</td></tr>
{{if .Expand}}<tr class="exp row-{{.Status}}" hidden><td colspan="6">{{if .Findings}}<table class="findings"><tbody>{{range .Findings}}<tr><td><span class="badge st-{{fclass .Class}}">{{.Class}}</span></td><td><code>{{.Rule}}</code></td><td><code>{{.Path}}</code></td><td>{{.Message}}</td></tr>{{end}}</tbody></table>{{end}}{{with .ConsumerRecord}}<p class="prov">consumer version <code>{{.Hash}}</code> · recorded {{.RecordedAt}} by {{.RecordedBy}}</p>{{end}}{{with .ProviderRecord}}<p class="prov">provider version <code>{{.Hash}}</code> · recorded {{.RecordedAt}} by {{.RecordedBy}}</p>{{end}}</td></tr>
{{end}}{{end}}</tbody>
</table>
</div>
</section>
{{else}}<div class="card"><p class="empty">no deploy records; run <code>wirefit record-deploy</code> in each service first</p></div>
{{end}}{{range .PromoGroups}}<section>
<h2>promotion {{.Pair}}<span class="gsum">{{.Summary}}</span></h2>
<div class="card">
<table>
<thead><tr><th>service</th><th>check</th><th>status</th><th>detail</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="row-{{.Status}}"{{if .Findings}} data-exp{{end}}><td>{{.Service}}</td><td>{{if .Check}}<code>{{.Check}}</code>{{end}}</td><td><span class="badge st-{{.Status}}" title="{{sthelp .Status}}">{{.Status}}</span></td><td class="detail">{{.Detail}}</td></tr>
{{if .Findings}}<tr class="exp row-{{.Status}}" hidden><td colspan="4"><table class="findings"><tbody>{{range .Findings}}<tr><td><span class="badge st-{{fclass .Class}}">{{.Class}}</span></td><td><code>{{.Rule}}</code></td><td><code>{{.Path}}</code></td><td>{{.Message}}</td></tr>{{end}}</tbody></table></td></tr>
{{end}}{{end}}</tbody>
</table>
</div>
</section>
{{end}}<footer>generated by <code>wirefit matrix</code></footer>
<script>
document.addEventListener("click", function (ev) {
  var row = ev.target.closest("tr[data-exp]");
  if (!row || ev.target.closest("a")) return;
  row.classList.toggle("open");
  row.nextElementSibling.hidden = !row.nextElementSibling.hidden;
});
</script>
<noscript><style>tr.exp { display: table-row; }</style></noscript>
</main>
</body>
</html>
`))

type matrixStatusCount struct {
	Status matrixStatus
	N      int
}

// matrixHTMLRow is one detail-table row: the edge plus whether it has
// findings or provenance to expand.
type matrixHTMLRow struct {
	matrixEdge
	Expand bool
}

type matrixEnvGroup struct {
	Env, Summary, ID string
	Rows             []matrixHTMLRow
}

// promoRow is one rendered promotion check; Check is empty for the
// per-service in-sync row.
type promoRow struct {
	Service, Check string
	Status         matrixStatus
	Detail         string
	Findings       []diff.Finding
}

type promoGroup struct {
	Pair    string // "dev → staging"
	Summary string
	Rows    []promoRow
}

type pipelineArrow struct {
	Status matrixStatus
	Title  string
}

// pipelineNode is one env in the health strip; Anchor is empty when the env
// has no section to link to, Arrow is nil after the last pipeline env and on
// envs outside the pipeline.
type pipelineNode struct {
	Env, Summary, Anchor string
	Status               matrixStatus
	Arrow                *pipelineArrow
}

type matrixPageData struct {
	EnvLabel, EdgeLabel, PromoLabel string
	Verdict, VerdictTone            string
	Counts                          []matrixStatusCount
	Pipeline                        []pipelineNode
	Groups                          []matrixEnvGroup
	PromoGroups                     []promoGroup
}

// matrixStatusHelp explains each status in plain words; shown as a tooltip
// wherever the status appears so the page does not assume wirefit vocabulary.
var matrixStatusHelp = map[matrixStatus]string{
	matrixStatusOK:           "the consumer can read everything the provider sends",
	matrixStatusWarning:      "works as deployed, but review the findings",
	matrixStatusIncompatible: "the consumer would fail to read what the provider sends",
	matrixStatusUntracked:    "one side has no deploy record, so there is nothing reliable to check against",
	matrixStatusError:        "the check could not run; re-publish and re-record",
}

// findingStatus maps a finding class onto the matrix status palette so the
// detail view reuses the st-* styles.
func findingStatus(c diff.Class) matrixStatus {
	switch c {
	case diff.Breaking:
		return matrixStatusIncompatible
	case diff.Warning:
		return matrixStatusWarning
	case diff.Safe:
		return matrixStatusOK
	}
	return matrixStatusUntracked
}

// anchorSlug makes a string safe for an id/fragment. Distinct inputs can
// collide ("a b" vs "a-b"); acceptable, env names are regex-constrained.
func anchorSlug(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
			return r
		}
		return '-'
	}, s)
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

func matrixCounts(statuses []matrixStatus) []matrixStatusCount {
	n := map[matrixStatus]int{}
	for _, s := range statuses {
		n[s]++
	}
	var out []matrixStatusCount
	for _, s := range matrixStatusOrder {
		if n[s] > 0 {
			out = append(out, matrixStatusCount{s, n[s]})
		}
	}
	return out
}

func edgeStatuses(edges []matrixEdge) []matrixStatus {
	out := make([]matrixStatus, len(edges))
	for i, e := range edges {
		out[i] = e.Status
	}
	return out
}

func countsSummary(counts []matrixStatusCount) string {
	var parts []string
	for _, c := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", c.N, c.Status))
	}
	return strings.Join(parts, " · ")
}

// promoGroups turns the promotion edges into template groups: one per
// adjacent env pair (input order, which is pipeline order), rows sorted
// worst-status-first like the env sections.
func promoGroups(promos []promoEdge) []promoGroup {
	var groups []promoGroup
	for start := 0; start < len(promos); {
		end := start
		for end < len(promos) && promos[end].From == promos[start].From && promos[end].To == promos[start].To {
			end++
		}
		pair := promos[start:end]
		g := promoGroup{Pair: pair[0].From + " → " + pair[0].To}
		statuses := make([]matrixStatus, len(pair))
		rows := make([]promoRow, len(pair))
		for i, p := range pair {
			statuses[i] = p.Status
			rows[i] = promoRow{Service: p.Service, Check: promoCheck(p), Status: p.Status, Detail: p.Detail, Findings: p.Findings}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if ra, rb := matrixStatusRank(rows[i].Status), matrixStatusRank(rows[j].Status); ra != rb {
				return ra < rb
			}
			if rows[i].Service != rows[j].Service {
				return rows[i].Service < rows[j].Service
			}
			return rows[i].Check < rows[j].Check
		})
		g.Summary = countsSummary(matrixCounts(statuses))
		g.Rows = rows
		groups = append(groups, g)
		start = end
	}
	return groups
}

// buildPipeline lays out the health strip: pipeline envs in promotion order
// with an arrow per adjacent pair, then any deploy-recorded envs outside the
// pipeline. Nil when there is neither a pipeline nor more than one env.
func buildPipeline(pipeline []string, groups []matrixEnvGroup, promos []promoEdge) []pipelineNode {
	if len(pipeline) == 0 && len(groups) < 2 {
		return nil
	}
	byEnv := map[string]*matrixEnvGroup{}
	for i := range groups {
		byEnv[groups[i].Env] = &groups[i]
	}
	node := func(env string) pipelineNode {
		n := pipelineNode{Env: env, Status: matrixStatusUntracked, Summary: "no deploy records"}
		if g := byEnv[env]; g != nil {
			// Rows are worst-first, so the first row carries the env's status.
			n.Anchor, n.Summary, n.Status = g.ID, g.Summary, g.Rows[0].Status
		}
		return n
	}
	var nodes []pipelineNode
	inPipe := map[string]bool{}
	for i, env := range pipeline {
		inPipe[env] = true
		n := node(env)
		if i+1 < len(pipeline) {
			n.Arrow = promoArrow(promos, env, pipeline[i+1])
		}
		nodes = append(nodes, n)
	}
	for _, g := range groups {
		if !inPipe[g.Env] {
			nodes = append(nodes, node(g.Env))
		}
	}
	return nodes
}

// promoArrow summarizes one adjacent env pair's promotion checks into the
// arrow between their strip nodes.
func promoArrow(promos []promoEdge, from, to string) *pipelineArrow {
	var statuses []matrixStatus
	for _, p := range promos {
		if p.From == from && p.To == to {
			statuses = append(statuses, p.Status)
		}
	}
	if len(statuses) == 0 {
		return &pipelineArrow{Status: matrixStatusUntracked,
			Title: fmt.Sprintf("promote %s → %s: no promotion checks", from, to)}
	}
	counts := matrixCounts(statuses)
	return &pipelineArrow{Status: counts[0].Status,
		Title: fmt.Sprintf("promote %s → %s: %s", from, to, countsSummary(counts))}
}

// renderMatrixHTML renders the matrix as a self-contained HTML page
// (`matrix --format html` / `-o *.html`): the pipeline health strip, then one
// section per env plus one per promotion pair, worst rows first, with the
// summary chips doubling as CSS-only status filters and rows expanding to
// their findings and deploy provenance.
func renderMatrixHTML(edges []matrixEdge, promos []promoEdge, pipeline []string) []byte {
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

	statuses := edgeStatuses(es)
	for _, p := range promos {
		statuses = append(statuses, p.Status)
	}
	d := matrixPageData{Counts: matrixCounts(statuses), EdgeLabel: plural(len(es), "edge"),
		PromoGroups: promoGroups(promos)}
	if len(promos) > 0 {
		d.PromoLabel = plural(len(promos), "promotion check")
	}
	for start := 0; start < len(es); {
		end := start
		for end < len(es) && es[end].Env == es[start].Env {
			end++
		}
		env := es[start].Env
		g := matrixEnvGroup{Env: env, ID: "env-" + anchorSlug(env)}
		g.Rows = make([]matrixHTMLRow, end-start)
		for i, e := range es[start:end] {
			g.Rows[i] = matrixHTMLRow{matrixEdge: e,
				Expand: len(e.Findings) > 0 || e.ConsumerRecord != nil || e.ProviderRecord != nil}
		}
		g.Summary = countsSummary(matrixCounts(edgeStatuses(es[start:end])))
		d.Groups = append(d.Groups, g)
		start = end
	}
	d.EnvLabel = plural(len(d.Groups), "env")
	d.Pipeline = buildPipeline(pipeline, d.Groups, promos)

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
