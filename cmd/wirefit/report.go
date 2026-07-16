package main

// Markdown report renderer (PRD 1.10 / 3.6): one body usable as a GitHub PR
// comment or GitLab MR note. CI adapters stay thin — they post this verbatim.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
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

// matrixPage is the self-contained hostable page: inline CSS plus one static
// inline script, no external resources, no timestamps (a page regenerated from
// the same repo state must not churn). The script carries no template actions,
// so html/template never escapes data into a JS context; all behavior reads
// DOM attributes. Without JS the noscript style renders every detail modal
// inline. Light/dark follows the viewer's system preference via the CSS
// variables.
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
  html { scrollbar-gutter: stable; }
  body { margin: 0; background: var(--bg); color: var(--text);
         font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
  body.modal-open { overflow: hidden; }
  main { max-width: 64rem; margin: 0 auto; padding: 2.5rem 1.25rem 4rem; }
  h1 { font-size: 1.3rem; margin: 0; }
  .sub { color: var(--muted); font-size: .88rem; margin: .2rem 0 1rem; }
  .chips { display: flex; flex-wrap: wrap; gap: .5rem; margin-bottom: 1.25rem; }
  .group-actions { display: flex; flex-wrap: wrap; gap: .5rem; margin: -.55rem 0 1.25rem; }
  .group-actions button { border: 1px solid var(--border); border-radius: 6px;
                          background: var(--card); color: var(--text); padding: .35rem .65rem;
                          font: inherit; font-size: .78rem; cursor: pointer; }
  .group-actions button:hover { background: var(--bg); }
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
          border-radius: 10px; max-height: min(65vh, 44rem); overflow: auto;
          box-shadow: 0 1px 3px rgba(31, 35, 40, .06); }
  table { border-collapse: collapse; width: 100%; min-width: 46rem; }
  th { text-align: left; font-size: .72rem; text-transform: uppercase;
       letter-spacing: .06em; color: var(--muted); font-weight: 600;
       padding: .7rem .95rem; border-bottom: 1px solid var(--border);
       position: sticky; top: 0; z-index: 1; background: var(--card); }
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
  tr[data-modal] { cursor: pointer; }
  table.findings { min-width: 0; width: 100%; }
  .findings td { border-bottom: none; padding: .35rem .7rem .35rem 0; font-size: .85rem; }
  .parties { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: .8rem;
             margin: 0; padding: .8rem 1.25rem; border-bottom: 1px solid var(--border); }
  .parties div { min-width: 0; }
  .parties dt { color: var(--muted); font-size: .68rem; font-weight: 600;
                letter-spacing: .06em; text-transform: uppercase; }
  .parties dd { margin: .15rem 0 0; overflow-wrap: anywhere; }
  .party-version { display: block; color: var(--muted); font-size: .75rem; margin-top: .15rem; }
  .msection, .provenance { padding: .8rem 1.25rem; border-bottom: 1px solid var(--border); }
  .msection h4 { margin: 0 0 .35rem; color: var(--muted); font-size: .68rem;
                 font-weight: 600; letter-spacing: .06em; text-transform: uppercase; }
  .prov { color: var(--muted); font-size: .8rem; margin: .25rem 0 0; }
  .prov:first-child { margin-top: 0; }
  dialog { background: var(--card); color: var(--text); border: 1px solid var(--border);
           border-radius: 8px; padding: 0; box-shadow: 0 18px 50px rgba(31, 35, 40, .28);
           width: min(76rem, calc(100vw - 2rem)); max-height: 88vh; overflow: auto;
           overscroll-behavior: contain; }
  dialog::backdrop { background: rgba(0, 0, 0, .45); }
  .dhead { position: sticky; top: 0; z-index: 2; display: flex; align-items: center;
           justify-content: space-between; gap: 1rem; padding: 1rem 1.25rem .85rem;
           background: var(--card); border-bottom: 1px solid var(--border); }
  .dhead h3 { margin: 0; font-size: 1rem; font-weight: 650; overflow-wrap: anywhere; }
  .dhead form { margin: 0; }
  .dhead button { width: 2rem; height: 2rem; border: 1px solid transparent; border-radius: 6px;
                  background: transparent; color: var(--muted); font: inherit;
                  font-size: 1.05rem; cursor: pointer; }
  .dhead button:hover { border-color: var(--border); background: var(--bg); color: var(--text); }
  .dsub { display: flex; flex-wrap: wrap; align-items: center; gap: .45rem;
          color: var(--muted); font-size: .85rem; margin: 0; padding: .75rem 1.25rem;
          background: var(--bg); border-bottom: 1px solid var(--border); }
  .bodies { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
            gap: 1rem; padding: 1rem 1.25rem 1.25rem; }
  @media (max-width: 54rem) {
    .parties, .bodies { grid-template-columns: 1fr; }
  }
  .bodies > div { min-width: 0; overflow: hidden; border: 1px solid var(--border);
                  border-radius: 8px; background: var(--bg); }
  .bodies h4 { margin: 0; padding: .65rem .85rem; background: var(--card);
               border-bottom: 1px solid var(--border); font-size: .72rem; text-transform: uppercase;
               letter-spacing: .06em; color: var(--muted); font-weight: 600; }
  .bodies pre { margin: 0; overflow: auto; max-height: 57vh; background: transparent;
                border: 0; border-radius: 0; padding: .85rem 1rem;
                font: .78rem/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; }
  .body-empty { color: var(--muted); font-size: .8rem; margin: 0; padding: .85rem 1rem; }
  .hl { display: inline-block; min-width: 100%; border-radius: 4px; padding: 0 .35rem; margin-left: -.35rem; }
  .ver code { color: var(--muted); }
  section { margin-bottom: 1.9rem; }
  h2 { font-size: .95rem; margin: 0 0 .55rem; }
  .group > details > summary { cursor: pointer; padding: .55rem .75rem;
                               background: var(--card); border: 1px solid var(--border);
                               border-radius: 10px; box-shadow: 0 1px 3px rgba(31, 35, 40, .06);
                               user-select: none; }
  .group > details[open] > summary { margin-bottom: .55rem; }
  .group > details > summary h2 { display: inline; margin: 0; }
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
{{if .Pipeline}}<nav class="pipeline">{{range .Pipeline}}{{if .Anchor}}<a class="pnode" href="#{{.Anchor}}"><span class="badge st-{{.Status}}">{{.Env}}</span><span class="psum">{{.Summary}}</span></a>{{else}}<span class="pnode"><span class="badge st-{{.Status}}">{{.Env}}</span><span class="psum">{{.Summary}}</span></span>{{end}}{{with .Arrow}} <span class="parrow pa-{{.Status}}" title="{{.Title}}">→</span> {{end}}{{end}}<span class="hint">badges = health per env · arrows = readiness or in-sync target health</span></nav>
{{end}}{{if .Verdict}}<p class="verdict {{.VerdictTone}}">{{.Verdict}}</p>
{{end}}{{if .Counts}}<div class="chips">{{range .Counts}}<label class="chip st-{{.Status}}" for="f-{{.Status}}" title="{{sthelp .Status}}">{{.N}} {{.Status}}</label>{{end}}<span class="hint">click a chip to hide or show that status · click a row for its detail view</span></div>
{{end}}{{if or .Groups .PromoGroups}}<div class="group-actions"><button type="button" data-groups="expand">Expand all</button><button type="button" data-groups="collapse">Collapse all</button><button type="button" data-groups="collapse-healthy">Collapse healthy</button></div>
{{end}}{{range .Groups}}<section class="group" id="{{.ID}}" data-healthy="{{if .Open}}false{{else}}true{{end}}">
<details{{if .Open}} open{{end}}><summary><h2>{{.Env}}<span class="gsum">{{.Summary}}</span></h2></summary>
<div class="card">
<table>
<thead><tr><th>consumer</th><th>version</th><th>provider / interaction</th><th>version</th><th>status</th><th>detail</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="row-{{.Status}}"{{with .Modal}} data-modal="{{.ID}}"{{end}}><td>{{.Consumer}}</td><td class="ver">{{with .ConsumerRecord}}<code title="{{.Hash}} · recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Label}}</code>{{end}}</td><td><code>{{.Provider}}/{{.Interaction}}</code></td><td class="ver">{{with .ProviderRecord}}<code title="{{.Hash}} · recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Label}}</code>{{end}}</td><td><span class="badge st-{{.Status}}" title="{{sthelp .Status}}">{{.Status}}</span></td><td class="detail">{{.Detail}}</td></tr>
{{end}}</tbody>
</table>
</div>
{{range .Rows}}{{with .Modal}}{{template "matrix-modal" .}}{{end}}{{end}}</details></section>
{{else}}<div class="card"><p class="empty">no deploy records; run <code>wirefit record-deploy</code> in each service first</p></div>
{{end}}{{range .PromoGroups}}<section class="group" id="{{.ID}}" data-healthy="{{if .Open}}false{{else}}true{{end}}">
<details{{if .Open}} open{{end}}><summary><h2>promotion {{.Pair}}<span class="gsum">{{.Summary}}</span></h2></summary>
<div class="card">
<table>
<thead><tr><th>service</th><th>check</th><th>status</th><th>detail</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="row-{{.Status}}"{{with .Modal}} data-modal="{{.ID}}"{{end}}><td>{{.Service}}</td><td>{{if .Check}}<code>{{.Check}}</code>{{end}}</td><td><span class="badge st-{{.Status}}" title="{{sthelp .Status}}">{{.Status}}</span></td><td class="detail">{{.Detail}}</td></tr>
{{end}}</tbody>
</table>
</div>
{{range .Rows}}{{with .Modal}}{{template "matrix-modal" .}}{{end}}{{end}}</details></section>
{{end}}<footer>generated by <code>wirefit matrix</code></footer>
<script>
document.addEventListener("click", function (ev) {
  var action = ev.target.closest("button[data-groups]");
  if (action) {
    var mode = action.getAttribute("data-groups");
    var selector = mode === "collapse-healthy"
      ? '.group[data-healthy="true"] > details'
      : ".group > details";
    document.querySelectorAll(selector).forEach(function (group) { group.open = mode === "expand"; });
    return;
  }
  var anchor = ev.target.closest('a[href^="#"]');
  if (anchor) {
    var target = document.getElementById(anchor.getAttribute("href").slice(1));
    if (target && target.matches(".group")) target.querySelector("details").open = true;
  }
  var dlg = ev.target.closest("dialog");
  if (dlg) {
    if (ev.target === dlg) {
      var box = dlg.getBoundingClientRect();
      if (ev.clientX < box.left || ev.clientX > box.right ||
          ev.clientY < box.top || ev.clientY > box.bottom) dlg.close();
    }
    return;
  }
  var row = ev.target.closest("tr[data-modal]");
  if (!row || ev.target.closest("a")) return;
  document.getElementById(row.getAttribute("data-modal")).showModal();
  document.body.classList.add("modal-open");
});
document.addEventListener("close", function (ev) {
  if (ev.target.matches("dialog") && !document.querySelector("dialog[open]")) {
    document.body.classList.remove("modal-open");
  }
}, true);
</script>
<noscript><style>dialog { display: block; position: static; margin: .8rem 0; max-height: none; width: auto; }</style></noscript>
</main>
</body>
</html>
{{define "matrix-modal"}}<dialog id="{{.ID}}" aria-labelledby="{{.ID}}-title">
<div class="dhead"><h3 id="{{.ID}}-title">{{.Title}}</h3><form method="dialog"><button aria-label="close" autofocus>✕</button></form></div>
<p class="dsub">{{.Scope}}{{with .Check}} · <code>{{.}}</code>{{end}} · <span class="badge st-{{.Status}}" title="{{sthelp .Status}}">{{.Status}}</span>{{with .Detail}}<span>{{.}}</span>{{end}}</p>
<dl class="parties"><div><dt>consumer</dt><dd><code>{{.Consumer}}</code><span class="party-version">version {{with .ConsumerRecord}}<code>{{.Label}}</code>{{else}}unavailable{{end}}</span></dd></div><div><dt>provider</dt><dd><code>{{.Provider}}</code><span class="party-version">version {{with .ProviderRecord}}<code>{{.Label}}</code>{{else}}unavailable{{end}}</span></dd></div><div><dt>interaction</dt><dd><code>{{.Interaction}}</code></dd></div></dl>
{{if .Findings}}<div class="msection"><h4>findings</h4><table class="findings"><tbody>{{range .Findings}}<tr><td><span class="badge st-{{fclass .Class}}">{{.Class}}</span></td><td><code>{{.Rule}}</code></td><td><code>{{.Path}}</code></td><td>{{.Message}}</td></tr>{{end}}</tbody></table></div>
{{end}}{{if or .ConsumerRecord .ProviderRecord}}<div class="provenance">{{with .ConsumerRecord}}<p class="prov">consumer version <code>{{.Label}}</code>{{if .Version}} · hash <code>{{.Hash}}</code>{{end}} · recorded {{.RecordedAt}} by {{.RecordedBy}}</p>
{{end}}{{with .ProviderRecord}}<p class="prov">provider version <code>{{.Label}}</code>{{if .Version}} · hash <code>{{.Hash}}</code>{{end}} · recorded {{.RecordedAt}} by {{.RecordedBy}}</p>{{end}}</div>
{{end}}{{if or .ConsumerLines .ProviderLines}}<div class="bodies">
<div><h4>{{.ConsumerBodyLabel}}</h4>{{if .ConsumerLines}}<pre>{{range .ConsumerLines}}{{if .Class}}<span class="hl st-{{.Class}}">{{.Text}}</span>{{else}}{{.Text}}{{end}}
{{end}}</pre>{{else}}<p class="body-empty">not available</p>{{end}}</div>
<div><h4>{{.ProviderBodyLabel}}</h4>{{if .ProviderLines}}<pre>{{range .ProviderLines}}{{if .Class}}<span class="hl st-{{.Class}}">{{.Text}}</span>{{else}}{{.Text}}{{end}}
{{end}}</pre>{{else}}<p class="body-empty">not available</p>{{end}}</div>
</div>
{{end}}</dialog>{{end}}
`))

type matrixStatusCount struct {
	Status matrixStatus
	N      int
}

type matrixHTMLModal struct {
	ID, Title, Scope, Check              string
	Consumer, Provider, Interaction      string
	ConsumerBodyLabel, ProviderBodyLabel string
	Status                               matrixStatus
	Detail                               string
	Findings                             []diff.Finding
	ConsumerRecord, ProviderRecord       *deployRecord
	ConsumerLines, ProviderLines         []bodyLine
}

// matrixHTMLRow is one detail-table row plus its optional modal.
type matrixHTMLRow struct {
	matrixEdge
	Modal *matrixHTMLModal
}

type matrixEnvGroup struct {
	Env, Summary, ID string
	Open             bool
	Rows             []matrixHTMLRow
}

// promoRow is one rendered promotion check; Check is empty for the
// per-service in-sync row.
type promoRow struct {
	promoEdge
	Check string
	Modal *matrixHTMLModal
}

type promoGroup struct {
	Pair    string // "dev → staging"
	Summary string
	ID      string
	Open    bool
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

// bodyLine is one rendered line of a body's canonical IR JSON form; Class is
// set on every line of a node a finding points at, "" otherwise.
type bodyLine struct {
	Text  string
	Class matrixStatus
}

// bodyMarks collapses findings to the worst class per path, in the matrix
// status palette, so bodySide can highlight the lines findings point at.
func bodyMarks(fs []diff.Finding) map[string]matrixStatus {
	worst := map[string]diff.Class{}
	for _, f := range fs {
		if c, ok := worst[f.Path]; !ok || f.Class > c {
			worst[f.Path] = f.Class
		}
	}
	marks := make(map[string]matrixStatus, len(worst))
	for p, c := range worst {
		marks[p] = findingStatus(c)
	}
	return marks
}

// bodySide renders a schema as its pretty-printed IR JSON, one bodyLine per
// line. Nodes are addressed with the finding path grammar ($.a.b[], <tag>,
// {}), so marks apply by exact string match; a marked node highlights its
// whole block. Property names are sorted, so normalized input renders
// deterministically (NF3).
func bodySide(s *ir.Schema, marks map[string]matrixStatus) []bodyLine {
	w := &bodyWriter{marks: marks}
	w.node(s, "$", "", 0, "")
	w.trimComma()
	return w.lines
}

type bodyWriter struct {
	lines []bodyLine
	marks map[string]matrixStatus
}

func (w *bodyWriter) line(indent int, text string, class matrixStatus) {
	w.lines = append(w.lines, bodyLine{Text: strings.Repeat("  ", indent) + text, Class: class})
}

// trimComma drops the trailing comma of the last emitted line: fields and
// list elements are emitted comma-terminated, and the enclosing container
// trims the final one before closing.
func (w *bodyWriter) trimComma() {
	if n := len(w.lines); n > 0 {
		w.lines[n-1].Text = strings.TrimSuffix(w.lines[n-1].Text, ",")
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func jsonStrs(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}

// node renders one schema as a comma-terminated object block, fields in
// ir.Schema declaration order. prefix is the `"key": ` heading the opening
// brace; class is inherited from marked ancestors.
func (w *bodyWriter) node(s *ir.Schema, path, prefix string, indent int, class matrixStatus) {
	if m, ok := w.marks[path]; ok {
		class = m
	}
	w.line(indent, prefix+"{", class)
	if s != nil {
		if s.Type != "" {
			w.line(indent+1, `"type": `+jsonStr(s.Type)+",", class)
		}
		if s.Scalar != "" {
			w.line(indent+1, `"x-ct-scalar": `+jsonStr(string(s.Scalar))+",", class)
		}
		if s.Nullable {
			w.line(indent+1, `"x-ct-nullable": true,`, class)
		}
		if s.Recursive {
			w.line(indent+1, `"x-ct-recursive": true,`, class)
		}
		if len(s.Properties) > 0 {
			w.line(indent+1, `"properties": {`, class)
			for _, name := range sortedKeys(s.Properties) {
				w.node(s.Properties[name], path+"."+name, jsonStr(name)+": ", indent+2, class)
			}
			w.trimComma()
			w.line(indent+1, "},", class)
		}
		if len(s.Required) > 0 {
			w.line(indent+1, `"required": `+jsonStrs(s.Required)+",", class)
		}
		if s.Items != nil {
			w.node(s.Items, path+"[]", `"items": `, indent+1, class)
		}
		if len(s.Enum) > 0 {
			w.line(indent+1, `"enum": `+jsonStrs(s.Enum)+",", class)
		}
		if len(s.OneOf) > 0 {
			w.line(indent+1, `"oneOf": [`, class)
			for _, b := range s.OneOf {
				w.node(b, path+"<"+b.DiscriminatorValue+">", "", indent+2, class)
			}
			w.trimComma()
			w.line(indent+1, "],", class)
		}
		if s.Discriminator != "" {
			w.line(indent+1, `"x-ct-discriminator": `+jsonStr(s.Discriminator)+",", class)
		}
		if s.DiscriminatorValue != "" {
			w.line(indent+1, `"x-ct-discriminator-value": `+jsonStr(s.DiscriminatorValue)+",", class)
		}
		if s.AdditionalProperties != nil {
			if v := s.MapValue(); v != nil {
				w.node(v, path+"{}", `"additionalProperties": `, indent+1, class)
			} else {
				w.line(indent+1, `"additionalProperties": true,`, class)
			}
		}
		w.trimComma()
	}
	w.line(indent, "},", class)
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

func groupOpen(statuses []matrixStatus) bool {
	for _, s := range statuses {
		switch s {
		case matrixStatusIncompatible, matrixStatusError, matrixStatusWarning:
			return true
		}
	}
	return false
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
		g.ID = "promo-" + anchorSlug(g.Pair)
		statuses := make([]matrixStatus, len(pair))
		rows := make([]promoRow, len(pair))
		for i, p := range pair {
			statuses[i] = p.Status
			rows[i] = promoRow{promoEdge: p, Check: promoCheck(p)}
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
		for i := range rows {
			r := &rows[i]
			hasBodies := r.ConsumerBody != nil || r.ProviderBody != nil
			if len(r.Findings) == 0 && r.ConsumerRecord == nil && r.ProviderRecord == nil && !hasBodies {
				continue
			}
			consumer, provider := promoParties(r.promoEdge)
			m := &matrixHTMLModal{
				ID:    fmt.Sprintf("d-promo-%s-%d", anchorSlug(g.Pair), i),
				Title: fmt.Sprintf("%s → %s/%s", consumer, provider, r.Interaction),
				Scope: "promotion " + g.Pair, Check: r.Check,
				Consumer: consumer, Provider: provider, Interaction: r.Interaction,
				Status: r.Status, Detail: r.Detail, Findings: r.Findings,
				ConsumerRecord: r.ConsumerRecord, ProviderRecord: r.ProviderRecord,
			}
			if r.Side == "provides" {
				m.ConsumerBodyLabel = "target consumer · " + r.To
				m.ProviderBodyLabel = "candidate provider · " + r.From
			} else {
				m.ConsumerBodyLabel = "candidate consumer · " + r.From
				m.ProviderBodyLabel = "target provider · " + r.To
			}
			marks := bodyMarks(r.Findings)
			if r.ConsumerBody != nil {
				m.ConsumerLines = bodySide(r.ConsumerBody, marks)
			}
			if r.ProviderBody != nil {
				m.ProviderLines = bodySide(r.ProviderBody, marks)
			}
			r.Modal = m
		}
		g.Summary = countsSummary(matrixCounts(statuses))
		g.Open = groupOpen(statuses)
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
	allInSync := true
	for _, p := range promos {
		if p.From == from && p.To == to {
			statuses = append(statuses, p.Status)
			allInSync = allInSync && p.InSync
		}
	}
	if len(statuses) == 0 {
		return &pipelineArrow{Status: matrixStatusUntracked,
			Title: fmt.Sprintf("promote %s → %s: no promotion checks", from, to)}
	}
	counts := matrixCounts(statuses)
	if allInSync {
		return &pipelineArrow{Status: counts[0].Status,
			Title: fmt.Sprintf("%s → %s: no pending contract changes · target compatibility: %s", from, to, countsSummary(counts))}
	}
	return &pipelineArrow{Status: counts[0].Status,
		Title: fmt.Sprintf("promote %s → %s: %s", from, to, countsSummary(counts))}
}

// renderMatrixHTML renders the matrix as a self-contained HTML page
// (`matrix --format html` / `-o *.html`): the pipeline health strip, then one
// section per env plus one per promotion pair, worst rows first, with the
// summary chips doubling as CSS-only status filters and rows opening a detail
// modal with findings, deploy provenance and the two bodies side by side.
func renderMatrixHTML(edges []matrixEdge, promos []promoEdge, pipeline []string) []byte {
	es := append([]matrixEdge(nil), edges...) // don't reorder the caller's slice
	envRank := make(map[string]int, len(pipeline))
	for i, env := range pipeline {
		if _, exists := envRank[env]; !exists {
			envRank[env] = i
		}
	}
	envLess := func(a, b string) bool {
		ra, aInPipeline := envRank[a]
		rb, bInPipeline := envRank[b]
		switch {
		case aInPipeline && bInPipeline:
			return ra < rb
		case aInPipeline != bInPipeline:
			return aInPipeline
		default:
			return a < b
		}
	}
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Env != b.Env {
			return envLess(a.Env, b.Env)
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
		g.Open = groupOpen(edgeStatuses(es[start:end]))
		g.Rows = make([]matrixHTMLRow, end-start)
		for i, e := range es[start:end] {
			row := matrixHTMLRow{matrixEdge: e}
			hasBodies := e.ConsumerBody != nil && e.ProviderBody != nil
			if len(e.Findings) > 0 || e.ConsumerRecord != nil || e.ProviderRecord != nil || hasBodies {
				row.Modal = &matrixHTMLModal{
					ID:    fmt.Sprintf("d-%s-%d", g.ID, i),
					Title: fmt.Sprintf("%s → %s/%s", e.Consumer, e.Provider, e.Interaction), Scope: e.Env,
					Consumer: e.Consumer, Provider: e.Provider, Interaction: e.Interaction,
					ConsumerBodyLabel: "consumer projection", ProviderBodyLabel: "provider schema",
					Status: e.Status, Detail: e.Detail, Findings: e.Findings,
					ConsumerRecord: e.ConsumerRecord, ProviderRecord: e.ProviderRecord,
				}
			}
			if hasBodies && row.Modal != nil {
				marks := bodyMarks(e.Findings)
				row.Modal.ConsumerLines = bodySide(e.ConsumerBody, marks)
				row.Modal.ProviderLines = bodySide(e.ProviderBody, marks)
			}
			g.Rows[i] = row
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
