package main

// HTML matrix report (`matrix --format html` / `-o *.html`): one self-contained
// page, no external resources, no timestamps (NF3: byte-identical re-render).
// The page is contract-centric: a landing view lists every contract (provider +
// interaction) and each contract gets its own view, switched via CSS :target so
// navigation needs no JS.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/wirefit/wirefit/internal/diff"
	"github.com/wirefit/wirefit/internal/ir"
)

// matrixPage is self-contained and deterministic. Fragment navigation works
// without JavaScript; the static script progressively adds directory filters.
var matrixPage = template.Must(template.New("matrix").Funcs(template.FuncMap{
	"fclass": findingStatus,
	"sthelp": func(s matrixStatus) string { return matrixStatusHelp[s] },
	"lower":  strings.ToLower,
	"shorttime": func(s string) string {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC().Format("02 Jan 2006, 15:04 UTC")
		}
		return s
	},
	"plural": plural,
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>WireFit compatibility</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f5f7fb; --surface: #ffffff; --surface-2: #f8faff;
    --border: #dce2ee; --text: #172033; --muted: #68748a;
    --accent: #4f46e5; --accent-soft: #eef2ff;
    --ok-bg: #e8f8ee; --ok-fg: #167342;
    --warn-bg: #fff5d6; --warn-fg: #8a5a00;
    --bad-bg: #ffebed; --bad-fg: #b4233c;
    --dim-bg: #eef1f6; --dim-fg: #5f6b7e;
    --shadow: 0 10px 30px rgba(27, 39, 69, .07);
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #0c1220; --surface: #131c2e; --surface-2: #182237;
      --border: #29354a; --text: #edf2fb; --muted: #9aa8be;
      --accent: #a5b4fc; --accent-soft: #222c50;
      --ok-bg: #163526; --ok-fg: #62d895;
      --warn-bg: #3b3017; --warn-fg: #f3c75d;
      --bad-bg: #3f2028; --bad-fg: #ff8797;
      --dim-bg: #252f42; --dim-fg: #a9b4c6;
      --shadow: 0 14px 34px rgba(0, 0, 0, .2);
    }
  }
  * { box-sizing: border-box; }
  html { scrollbar-gutter: stable; }
  body { margin: 0; background: var(--bg); color: var(--text);
         font: 15px/1.55 system-ui, -apple-system, "Segoe UI", sans-serif; }
  a { color: var(--accent); }
  a:focus-visible, button:focus-visible, input:focus-visible, select:focus-visible,
  summary:focus-visible { outline: 3px solid var(--accent);
                          outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
                          outline-offset: 2px; border-radius: 5px; }
  main { max-width: 78rem; margin: 0 auto; padding: 1.25rem 1.25rem 4rem; }
  h1 { font-size: clamp(1.65rem, 3vw, 2.25rem); line-height: 1.15; letter-spacing: -.025em; margin: 0; }
  h2 { font-size: 1.15rem; margin: 0; }
  h3 { font-size: .78rem; text-transform: uppercase; letter-spacing: .08em;
       color: var(--muted); font-weight: 700; margin: 2rem 0 .65rem; }
  .sub { color: var(--muted); font-size: .92rem; margin: .35rem 0 0; }
  .shell { display: flex; align-items: center; justify-content: space-between; gap: 1rem;
           margin-bottom: 2.3rem; }
  .brand { color: var(--text); text-decoration: none; font-weight: 800; letter-spacing: -.03em; }
  .brand span { color: var(--accent); }
  .topnav { display: flex; gap: .25rem; padding: .25rem; border: 1px solid var(--border);
            border-radius: 10px; background: var(--surface); box-shadow: 0 2px 9px rgba(27,39,69,.04); }
  .topnav a { padding: .4rem .75rem; border-radius: 7px; color: var(--muted);
              font-size: .86rem; font-weight: 650; text-decoration: none; }
  .topnav a:hover { color: var(--text); background: var(--surface-2); }
  .topnav a[aria-current="page"] { color: var(--accent); background: var(--accent-soft); }
  .view { display: none; scroll-margin-top: 1rem; }
  #view-overview { display: block; }
  .view:target { display: block; }
  .view:target ~ #view-overview { display: none; }
  .breadcrumbs { display: flex; flex-wrap: wrap; gap: .35rem; align-items: center;
                 margin: 0 0 .75rem; color: var(--muted); font-size: .82rem; }
  .breadcrumbs a { color: var(--muted); text-decoration: none; }
  .breadcrumbs a:hover { color: var(--accent); }
  .breadcrumbs .back-link { display: inline-flex; align-items: center; justify-content: center;
                            width: 1.8rem; height: 1.8rem; margin-right: .15rem; border-radius: 7px;
                            color: var(--muted); font-size: 1rem; }
  .breadcrumbs .back-link:hover { background: var(--surface-2); color: var(--accent); }
  .pagehead { display: flex; align-items: flex-start; justify-content: space-between;
              gap: 1rem; margin-bottom: 1.5rem; }
  .pagehead-status { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: .45rem; }
  .hero { display: grid; grid-template-columns: minmax(0, 1.5fr) minmax(15rem, .8fr);
          gap: 1rem; margin-bottom: 1rem; }
  .hero-main { padding: 1.55rem 1.65rem; border: 1px solid var(--border); border-radius: 16px;
               background: linear-gradient(135deg, var(--surface), var(--accent-soft)); box-shadow: var(--shadow); }
  .eyebrow { color: var(--accent); font-size: .74rem; font-weight: 750; text-transform: uppercase;
             letter-spacing: .09em; margin: 0 0 .35rem; }
  .verdict { font-size: clamp(1.2rem, 2.5vw, 1.65rem); font-weight: 750; margin: 0; }
  .hero-side { display: grid; grid-template-columns: 1fr 1fr; gap: .7rem; }
  .metric { padding: 1rem; border: 1px solid var(--border); border-radius: 13px;
            background: var(--surface); box-shadow: 0 5px 18px rgba(27,39,69,.05); }
  .metric strong { display: block; font-size: 1.35rem; line-height: 1.1; }
  .metric span { color: var(--muted); font-size: .78rem; }
  .section-head { display: flex; align-items: end; justify-content: space-between; gap: 1rem;
                  margin: 2rem 0 .7rem; }
  .section-head h2 { font-size: 1.05rem; }
  .section-head p { color: var(--muted); margin: 0; font-size: .82rem; }
  .chips { display: flex; flex-wrap: wrap; gap: .45rem; }
  .chip, .badge { display: inline-flex; align-items: center; gap: .45rem;
                  border-radius: 999px; padding: .18rem .7rem;
                  font-size: .76rem; font-weight: 650; white-space: nowrap;
                  background: var(--dim-bg); color: var(--dim-fg); }
  .chip::before, .badge::before { content: ""; width: .5em; height: .5em;
                  border-radius: 50%; background: currentColor; flex: none; }
  .st-ok { background: var(--ok-bg); color: var(--ok-fg); }
  .st-warning { background: var(--warn-bg); color: var(--warn-fg); }
  .st-INCOMPATIBLE, .st-error { background: var(--bad-bg); color: var(--bad-fg); }
  .st-untracked { background: var(--dim-bg); color: var(--dim-fg); }
  .card { background: var(--surface); border: 1px solid var(--border);
          border-radius: 13px; overflow: auto; box-shadow: var(--shadow); }
  table { border-collapse: collapse; width: 100%; }
  th { text-align: left; font-size: .72rem; text-transform: uppercase;
       letter-spacing: .06em; color: var(--muted); font-weight: 600;
       padding: .7rem .95rem; border-bottom: 1px solid var(--border);
       background: var(--surface-2); }
  td { padding: .62rem .95rem; border-bottom: 1px solid var(--border);
       vertical-align: top; font-size: .9rem; }
  td a { font-weight: 620; text-decoration: none; }
  td a:hover { text-decoration: underline; }
  tbody tr:last-child td { border-bottom: none; }
  tbody tr:hover td { background: var(--bg); }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .84em; }
  td > code { white-space: nowrap; }
  .envcell .badge { margin: 0 .3rem .15rem 0; }
  .pipeline { display: flex; flex-wrap: wrap; align-items: center; gap: .65rem;
              padding: 1rem 1.1rem; border: 1px solid var(--border); border-radius: 13px;
              background: var(--surface); box-shadow: var(--shadow); }
  .pnode { display: inline-flex; align-items: baseline; gap: .4rem; }
  .pnode .psum { color: var(--muted); font-size: .75rem; }
  .parrow { font-weight: 600; }
  .pa-ok { color: var(--ok-fg); }
  .pa-warning { color: var(--warn-fg); }
  .pa-INCOMPATIBLE, .pa-error { color: var(--bad-fg); }
  .pa-untracked { color: var(--dim-fg); }
  .filters { display: grid; grid-template-columns: minmax(12rem, 1fr) auto auto; gap: .65rem;
             align-items: end; margin: 0 0 .8rem; }
  .filters[hidden] { display: none; }
  .filters label, .filter-group { color: var(--muted); font-size: .74rem; font-weight: 650; }
  .filters input, .filters select { display: block; width: 100%; margin-top: .25rem;
                  border: 1px solid var(--border); border-radius: 8px; padding: .48rem .65rem;
                  background: var(--surface); color: var(--text); font: inherit; font-size: .85rem; }
  .filter-buttons { display: flex; flex-wrap: wrap; gap: .3rem; margin-top: .25rem; }
  .filter-buttons button { border: 1px solid var(--border); border-radius: 999px; padding: .38rem .65rem;
                           background: var(--surface); color: var(--muted); font: inherit;
                           font-size: .76rem; cursor: pointer; }
  .filter-buttons button[aria-pressed="true"] { color: var(--accent); border-color: var(--accent);
                                                 background: var(--accent-soft); }
  .empty-filter { display: none; padding: 2rem; text-align: center; color: var(--muted); }
  .relation { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
              align-items: center; gap: .8rem; padding: 1.2rem; }
  .relation .arrow { color: var(--muted); font-weight: 700; }
  .relation-card { padding: .85rem 1rem; border-radius: 10px; background: var(--surface-2); }
  .relation-card span { display: block; color: var(--muted); font-size: .72rem; text-transform: uppercase;
                        letter-spacing: .06em; }
  .relation-card strong { overflow-wrap: anywhere; }
  .detail-meta { display: flex; flex-wrap: wrap; gap: .45rem; padding: 0 1.2rem 1.2rem; }
  .attention-groups { display: grid; gap: .8rem; }
  .attention-group { overflow: hidden; border: 1px solid var(--border); border-radius: 13px;
                     background: var(--surface); box-shadow: var(--shadow); }
  .attention-group.is-clear .group-head { border-bottom: 0; }
  .group-head { display: flex; align-items: center; justify-content: space-between; gap: 1rem;
                padding: .7rem 1rem; border-bottom: 1px solid var(--border); background: var(--surface-2); }
  .group-head div { display: flex; align-items: baseline; gap: .55rem; }
  .group-head span:not(.badge) { color: var(--muted); font-size: .78rem; }
  .findings { min-width: 44rem; }
  .disclosure { margin-top: 1rem; border: 1px solid var(--border); border-radius: 13px;
                background: var(--surface); box-shadow: var(--shadow); overflow: hidden; }
  .disclosure summary { cursor: pointer; padding: .85rem 1rem; font-weight: 700; }
  .disclosure[open] summary { border-bottom: 1px solid var(--border); }
  .provenance { padding: .8rem 1rem; color: var(--muted); font-size: .82rem; }
  .prov { margin: .25rem 0; }
  .bodies { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
            gap: 1rem; padding: 1rem; }
  .bodies > div { min-width: 0; overflow: hidden; border: 1px solid var(--border);
                  border-radius: 8px; background: var(--bg); }
  .bodies h4 { margin: 0; padding: .65rem .85rem; background: var(--surface);
               border-bottom: 1px solid var(--border); font-size: .72rem; text-transform: uppercase;
               letter-spacing: .06em; color: var(--muted); font-weight: 600; }
  .bodies pre { margin: 0; overflow: auto; max-height: 57vh; background: transparent;
                border: 0; border-radius: 0; padding: .85rem 1rem;
                font: .78rem/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; }
  .body-empty { color: var(--muted); font-size: .8rem; margin: 0; padding: .85rem 1rem; }
  .hl { display: inline-block; min-width: 100%; border-radius: 4px; padding: 0 .35rem; margin-left: -.35rem; }
  .ver code { color: var(--muted); }
  .detail { color: var(--muted); font-size: .85rem; }
  .empty { color: var(--muted); text-align: center; padding: 2.2rem 1rem; margin: 0; }
  footer { color: var(--muted); font-size: .78rem; margin-top: 2.5rem; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px;
             overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; border: 0; }
  @media (max-width: 64rem) { .hero { grid-template-columns: 1fr; } .bodies { grid-template-columns: 1fr; } }
  @media (max-width: 48rem) {
    main { padding: .85rem .75rem 3rem; }
    .shell, .pagehead { align-items: stretch; flex-direction: column; }
    .topnav { width: 100%; }
    .topnav a { flex: 1; text-align: center; }
    .pagehead-status { justify-content: flex-start; }
    .hero-side { grid-template-columns: 1fr 1fr; }
    .filters { grid-template-columns: 1fr; }
    .relation { grid-template-columns: 1fr; }
    .relation .arrow { transform: rotate(90deg); justify-self: center; }
    table.responsive thead { display: none; }
    table.responsive, table.responsive tbody, table.responsive tr, table.responsive td { display: block; width: 100%; }
    table.responsive tr { padding: .65rem .8rem; border-bottom: 1px solid var(--border); }
    table.responsive tr:last-child { border-bottom: 0; }
    table.responsive td { display: grid; grid-template-columns: 7rem minmax(0,1fr); gap: .6rem;
                          border: 0; padding: .3rem 0; }
    table.responsive td::before { content: attr(data-label); color: var(--muted); font-size: .7rem;
                                  font-weight: 700; text-transform: uppercase; letter-spacing: .05em; }
    table.responsive tbody tr:hover td { background: transparent; }
  }
  @media (prefers-reduced-motion: reduce) { * { scroll-behavior: auto !important; } }
</style>
</head>
<body>
<main>
{{range .Views}}{{template "contract" .}}
{{end}}{{range .ServiceViews}}{{template "service" .}}
{{end}}{{range .Details}}{{template "detail-view" .}}
{{end}}{{template "contracts" .}}
{{template "services" .}}
{{template "overview" .}}
<footer>generated by <code>wirefit matrix</code></footer>
<script>
document.querySelectorAll("[data-directory]").forEach(function (dir) {
  var filters = dir.querySelector(".filters");
  if (!filters) return;
  filters.hidden = false;
  var search = filters.querySelector("[data-search]");
  var env = filters.querySelector("[data-env]");
  var buttons = Array.from(filters.querySelectorAll("button[data-status]"));
  function apply() {
    var q = search.value.trim().toLowerCase();
    var active = buttons.filter(function (b) { return b.getAttribute("aria-pressed") === "true"; })
                        .map(function (b) { return b.dataset.status; });
    var shown = 0;
    dir.querySelectorAll("[data-filter-row]").forEach(function (row) {
      var visible = (!q || row.dataset.search.indexOf(q) !== -1) &&
        (!env.value || row.dataset.envs.split(" ").indexOf(env.value) !== -1) &&
        active.indexOf(row.dataset.status) !== -1;
      row.hidden = !visible;
      if (visible) shown++;
    });
    dir.querySelector(".empty-filter").style.display = shown ? "none" : "block";
  }
  search.addEventListener("input", apply);
  env.addEventListener("change", apply);
  buttons.forEach(function (button) {
    button.addEventListener("click", function () {
      button.setAttribute("aria-pressed", button.getAttribute("aria-pressed") === "true" ? "false" : "true");
      apply();
    });
  });
});
</script>
</main>
</body>
</html>
{{define "shell"}}<header class="shell"><a class="brand" href="#view-overview">Wire<span>Fit</span></a><nav class="topnav" aria-label="Primary"><a href="#view-overview"{{if eq . "overview"}} aria-current="page"{{end}}>Overview</a><a href="#view-contracts"{{if eq . "contracts"}} aria-current="page"{{end}}>Contracts</a><a href="#view-services"{{if eq . "services"}} aria-current="page"{{end}}>Services</a></nav></header>{{end}}
{{define "status"}}<span class="badge st-{{.}}" aria-label="{{.}}: {{sthelp .}}" title="{{sthelp .}}">{{.}}</span>{{end}}
{{define "overview"}}<section class="view" id="view-overview">
{{template "shell" "overview"}}
<div class="hero"><div class="hero-main"><p class="eyebrow">Deployed compatibility</p><h1>Compatibility overview</h1><p class="verdict">{{if .Verdict}}{{.Verdict}}{{else}}No deployed edges to check{{end}}</p><p class="sub">{{.ContractLabel}} · {{.ServiceLabel}} · {{.EnvLabel}}</p></div>
<div class="hero-side"><div class="metric"><strong>{{.Failing}}</strong><span>deployed blockers</span></div><div class="metric"><strong>{{.Warnings}}</strong><span>deployed warnings</span></div><div class="metric"><strong>{{.TrackedContracts}}</strong><span>tracked contracts</span></div><div class="metric"><strong>{{.PromotionIssues}}</strong><span>promotion issues</span></div></div></div>
{{if .Pipeline}}<div class="section-head"><div><h2>Environment pipeline</h2><p>Deployed health on nodes; promotion readiness on arrows.</p></div></div><nav class="pipeline" aria-label="Environment pipeline">{{range .Pipeline}}<span class="pnode">{{template "status" .Status}}<strong>{{.Env}}</strong><span class="psum">{{.Summary}}</span></span>{{with .Arrow}}<span class="parrow pa-{{.Status}}" title="{{.Title}}">→</span>{{end}}{{end}}</nav>{{end}}
<div class="section-head"><div><h2>Needs attention</h2><p>All non-OK deployed relationships and promotion checks, worst first.</p></div></div>
{{if .Attention}}<div class="card"><table class="responsive"><caption class="sr-only">Compatibility results needing attention</caption><thead><tr><th>type</th><th>scope</th><th>relationship</th><th>status</th><th>detail</th></tr></thead><tbody>{{range .Attention}}<tr><td data-label="type">{{.Kind}}</td><td data-label="scope">{{.Scope}}</td><td data-label="relationship">{{if .Href}}<a href="#{{.Href}}"><code>{{.Primary}}</code></a>{{else}}<code>{{.Primary}}</code>{{end}}{{with .Secondary}}<div class="detail">{{.}}</div>{{end}}</td><td data-label="status">{{template "status" .Status}}</td><td data-label="detail" class="detail">{{.Detail}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="card"><p class="empty">nothing needs attention</p></div>{{end}}
</section>{{end}}
{{define "contracts"}}<section class="view directory" id="view-contracts" data-directory>
{{template "shell" "contracts"}}<div class="pagehead"><div><h1>Contracts</h1><p class="sub">Provider-owned interactions and their deployed consumers.</p></div><div class="pagehead-status">{{range .Counts}}<span class="chip st-{{.Status}}" aria-label="{{.N}} {{.Status}} contracts">{{.N}} {{.Status}}</span>{{end}}</div></div>
{{template "filters" .ContractFilters}}
{{if .Contracts}}<div class="card"><table class="responsive"><caption class="sr-only">Contracts</caption><thead><tr><th>contract</th><th>consumers</th><th>environments</th><th>deployed health</th>{{if .HasPromotion}}<th>promotion readiness</th>{{end}}</tr></thead><tbody>{{range .Contracts}}<tr data-filter-row data-search="{{lower .Provider}} {{lower .Interaction}}" data-status="{{.Status}}" data-envs="{{range .Envs}}{{.Env}} {{end}}"><td data-label="contract"><a href="#{{.Slug}}"><code>{{.Provider}} / {{.Interaction}}</code></a></td><td data-label="consumers">{{.Consumers}}</td><td data-label="environments" class="envcell">{{range .Envs}}<span class="badge st-{{.Status}}">{{.Env}}</span>{{end}}</td><td data-label="deployed health">{{template "status" .HealthStatus}}</td>{{if $.HasPromotion}}<td data-label="promotion readiness">{{if .HasPromotion}}{{template "status" .PromoStatus}}{{else}}<span class="detail">not checked</span>{{end}}</td>{{end}}</tr>{{end}}</tbody></table><p class="empty-filter">No contracts match these filters.</p></div>{{else}}<div class="card"><p class="empty">no deploy records; run <code>wirefit record-deploy</code> in each service first</p></div>{{end}}</section>{{end}}
{{define "filters"}}<div class="filters" hidden><label>Search<input type="search" data-search placeholder="Name or interaction"></label><label>Environment<select data-env><option value="">All environments</option>{{range .Envs}}<option>{{.}}</option>{{end}}</select></label><div class="filter-group">Status<div class="filter-buttons">{{range .Counts}}<button type="button" data-status="{{.Status}}" aria-pressed="true">{{.Status}}</button>{{end}}</div></div></div>{{end}}
{{define "services"}}<section class="view" id="view-services">
{{template "shell" "services"}}<div class="directory" data-directory><div class="pagehead"><div><h1>Services</h1><p class="sub">Deploy-recorded services and the contracts they own or consume.</p></div></div>{{template "filters" .ServiceFilters}}
{{if .Services}}<div class="card"><table class="responsive"><caption class="sr-only">Services</caption>
<thead><tr><th>service</th><th>role</th><th>provides</th><th>consumes</th><th>envs</th><th>status</th></tr></thead>
<tbody>
{{range .Services}}<tr data-filter-row data-search="{{lower .Service}} {{.Role}}" data-status="{{.Status}}" data-envs="{{range .Envs}}{{.Env}} {{end}}"><td data-label="service"><a href="#{{.Slug}}"><code>{{.Service}}</code></a></td><td data-label="role">{{.Role}}</td><td data-label="provides">{{.Provides}}</td><td data-label="consumes">{{.Consumes}}</td><td data-label="environments" class="envcell">{{range .Envs}}<span class="badge st-{{.Status}}">{{.Env}}</span>{{end}}</td><td data-label="status">{{template "status" .Status}}</td></tr>
{{end}}</tbody>
</table><p class="empty-filter">No services match these filters.</p>
</div>
{{else}}<div class="card"><p class="empty">no deploy records; run <code>wirefit record-deploy</code> in each service first</p></div>
{{end}}</div></section>{{end}}
{{define "service"}}<section class="view" id="{{.Slug}}">
{{template "shell" "services"}}<nav class="breadcrumbs" aria-label="Breadcrumb"><a class="back-link" href="#view-services" aria-label="Back to services" title="Back to services"><span aria-hidden="true">←</span></a><a href="#view-services">Services</a><span>/</span><span>{{.Service}}</span></nav><div class="pagehead"><div><h1><code>{{.Service}}</code></h1><p class="sub">{{.Role}} · {{plural .Provides "provided interaction"}} · {{plural .Consumes "consumed contract"}}</p></div><div class="pagehead-status"><span class="detail">Deployed</span>{{template "status" .Status}}</div></div>
<h3>Deployed issues</h3>
{{if .DeployedIssues}}<div class="card"><table class="responsive"><caption class="sr-only">Deployed compatibility issues involving {{.Service}}</caption><thead><tr><th>where</th><th>role</th><th>relationship</th><th>status</th><th>why</th></tr></thead><tbody>{{range .DeployedIssues}}<tr><td data-label="where">{{.Scope}}</td><td data-label="role">{{.Role}}</td><td data-label="relationship">{{if .DetailSlug}}<a href="#{{.DetailSlug}}"><code>{{.Relationship}}</code></a>{{else}}<code>{{.Relationship}}</code>{{end}}</td><td data-label="status">{{template "status" .Status}}</td><td data-label="why" class="detail">{{.Detail}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="card"><p class="empty">No deployed compatibility issues.</p></div>{{end}}
{{if .HasPromotionChecks}}<h3>Promotion readiness</h3>
{{if .PromotionGroups}}<div class="attention-groups">{{range .PromotionGroups}}<section class="attention-group{{if not .Rows}} is-clear{{end}}"><div class="group-head"><div><strong>{{.Label}}</strong><span>{{.Summary}}</span></div><span class="badge st-{{.Status}}" aria-label="{{.Outcome}}: {{.Summary}}" title="{{.Summary}}">{{.Outcome}}</span></div>{{if .Rows}}<table class="responsive"><caption class="sr-only">Promotion issues from {{.From}} to {{.To}}</caption><thead><tr><th>role</th><th>relationship</th><th>status</th><th>why</th></tr></thead><tbody>{{range .Rows}}<tr><td data-label="role">{{.Role}}</td><td data-label="relationship">{{if .DetailSlug}}<a href="#{{.DetailSlug}}"><code>{{.Relationship}}</code></a>{{else}}<code>{{.Relationship}}</code>{{end}}</td><td data-label="status">{{template "status" .Status}}</td><td data-label="why" class="detail">{{.Detail}}</td></tr>{{end}}</tbody></table>{{end}}</section>{{end}}</div>{{else}}<div class="card"><p class="empty">No promotion checks were evaluated.</p></div>{{end}}{{end}}
<h3>Deployments</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Deployments for {{.Service}}</caption>
<thead><tr><th>env</th><th>recorded</th><th>by</th><th>provides</th><th>consumes</th><th>status</th></tr></thead>
<tbody>
{{range .EnvRows}}<tr><td data-label="environment">{{.Env}}</td><td data-label="recorded"><time datetime="{{.RecordedAt}}">{{shorttime .RecordedAt}}</time></td><td data-label="recorded by">{{.RecordedBy}}</td><td data-label="provides">{{.Provides}}</td><td data-label="consumes">{{.Consumes}}</td><td data-label="status">{{template "status" .Status}}{{if .Stale}} <span class="badge st-warning">stale</span>{{end}}</td></tr>
{{end}}</tbody>
</table>
</div>
{{if .ProvideRows}}<h3>Provides</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Contracts provided by {{.Service}}</caption><thead><tr><th>interaction</th><th>deployed versions and contract health</th></tr></thead>
<tbody>
{{range .ProvideRows}}<tr><td data-label="interaction"><a href="#{{.ContractSlug}}"><code>{{.Label}}</code></a></td><td data-label="environments" class="envcell">{{range .Envs}}<span class="badge st-{{.Status}}" title="{{.Title}}">{{.Env}} {{.Version}}</span>{{end}}</td></tr>
{{end}}</tbody>
</table>
</div>
{{end}}{{if .ConsumeRows}}<h3>Consumes</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Contracts consumed by {{.Service}}</caption><thead><tr><th>provider / interaction</th><th>service-specific compatibility</th></tr></thead>
<tbody>
{{range .ConsumeRows}}<tr><td data-label="contract"><a href="#{{.ContractSlug}}"><code>{{.Label}}</code></a></td><td data-label="environments" class="envcell">{{range .Envs}}{{if .DetailSlug}}<a class="badge st-{{.Status}}" href="#{{.DetailSlug}}" title="{{.Title}}">{{.Env}} {{.Version}}</a>{{else}}<span class="badge st-{{.Status}}" title="{{.Title}}">{{.Env}} {{.Version}}</span>{{end}}{{end}}</td></tr>
{{end}}</tbody>
</table>
</div>
{{end}}</section>{{end}}
{{define "contract"}}<section class="view" id="{{.Slug}}">
{{template "shell" "contracts"}}<nav class="breadcrumbs" aria-label="Breadcrumb"><a class="back-link" href="#view-contracts" aria-label="Back to contracts" title="Back to contracts"><span aria-hidden="true">←</span></a><a href="#view-contracts">Contracts</a><span>/</span><a href="#{{.ProviderSlug}}">{{.Provider}}</a><span>/</span><span>{{.Interaction}}</span></nav><div class="pagehead"><div><h1><code>{{.Interaction}}</code></h1><p class="sub">Provided by <a href="#{{.ProviderSlug}}"><code>{{.Provider}}</code></a> · {{plural .Consumers "consumer"}}</p></div><div class="pagehead-status"><span class="detail">Deployed</span>{{template "status" .HealthStatus}}{{if .HasPromotion}}<span class="detail">Promotion</span>{{template "status" .PromoStatus}}{{end}}</div></div>
{{if .EnvRows}}<h3>Environment summary</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Environment summary for {{.Provider}} / {{.Interaction}}</caption>
<thead><tr><th>env</th><th>provider version</th><th>consumers</th><th>status</th>{{if .Promotion}}<th>promotion</th>{{end}}</tr></thead>
<tbody>
{{range .EnvRows}}<tr><td data-label="environment">{{.Env}}</td><td data-label="provider version" class="ver">{{with .ProviderVersion}}<code title="{{.Hash}} · recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Label}}</code>{{end}}</td><td data-label="consumers">{{.Consumers}}</td><td data-label="deployed health">{{template "status" .Status}}</td>{{if $.Promotion}}<td data-label="promotion">{{if .NextEnv}}<span class="detail">to {{.NextEnv}}</span> {{template "status" .PromoStatus}}{{with .PromoDetail}}<div class="detail">{{.}}</div>{{end}}{{else}}<span class="detail">final environment</span>{{end}}</td>{{end}}</tr>
{{end}}</tbody>
</table>
</div>
{{end}}{{if .EnvSections}}<h3>Deployed relationships</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Deployed consumer relationships</caption><thead><tr><th>environment</th><th>consumer</th><th>consumer version</th><th>status</th><th>detail</th></tr></thead><tbody>{{range .EnvSections}}{{range .Rows}}<tr><td data-label="environment">{{.Env}}</td><td data-label="consumer">{{if .Modal}}<a href="#{{.Modal.ID}}"><code>{{.Consumer}}</code></a>{{else}}<code>{{.Consumer}}</code>{{end}}</td><td data-label="consumer version" class="ver">{{with .ConsumerRecord}}<code title="{{.Hash}} · recorded {{.RecordedAt}} by {{.RecordedBy}}">{{.Label}}</code>{{end}}</td><td data-label="status">{{template "status" .Status}}</td><td data-label="detail" class="detail">{{.Detail}}</td></tr>{{end}}{{end}}</tbody>
</table>
</div>
{{end}}{{if .PromoSections}}<h3>Promotion checks</h3>
<div class="card">
<table class="responsive"><caption class="sr-only">Promotion checks</caption><thead><tr><th>promotion</th><th>service</th><th>check</th><th>status</th><th>detail</th></tr></thead><tbody>{{range .PromoSections}}{{$pair := .Pair}}{{range .Rows}}<tr><td data-label="promotion">{{$pair}}</td><td data-label="service">{{.Service}}</td><td data-label="check">{{if .Modal}}<a href="#{{.Modal.ID}}"><code>{{.Check}}</code></a>{{else}}{{if .Check}}<code>{{.Check}}</code>{{end}}{{end}}</td><td data-label="status">{{template "status" .Status}}</td><td data-label="detail" class="detail">{{.Detail}}</td></tr>{{end}}{{end}}</tbody>
</table>
</div>
{{end}}</section>{{end}}
{{define "detail-view"}}<section class="view" id="{{.ID}}">{{template "shell" ""}}<nav class="breadcrumbs" aria-label="Breadcrumb"><a class="back-link" href="#{{.ContractSlug}}" aria-label="Back to contract" title="Back to contract"><span aria-hidden="true">←</span></a>{{if .ConsumerSlug}}<a href="#{{.ConsumerSlug}}">{{.Consumer}}</a>{{else}}<span>{{.Consumer}}</span>{{end}}<span>/</span><a href="#{{.ContractSlug}}">{{.Provider}} / {{.Interaction}}</a><span>/</span><span>{{.Scope}}</span></nav><div class="pagehead"><div><p class="eyebrow">{{if .Check}}Promotion compatibility{{else}}Deployed compatibility{{end}}</p><h1>{{.Title}}</h1><p class="sub">{{.Scope}}{{with .Check}} · <code>{{.}}</code>{{end}}</p></div><div class="pagehead-status">{{template "status" .Status}}</div></div>
<div class="card"><div class="relation"><div class="relation-card"><span>Consumer</span><strong>{{if .ConsumerSlug}}<a href="#{{.ConsumerSlug}}"><code>{{.Consumer}}</code></a>{{else}}<code>{{.Consumer}}</code>{{end}}</strong><div class="detail">version {{with .ConsumerRecord}}<code>{{.Label}}</code>{{else}}unavailable{{end}}</div></div><span class="arrow">→</span><div class="relation-card"><span>Provider / interaction</span><strong><a href="#{{.ContractSlug}}"><code>{{.Provider}} / {{.Interaction}}</code></a></strong><div class="detail">version {{with .ProviderRecord}}<code>{{.Label}}</code>{{else}}unavailable{{end}}</div></div></div>{{with .Detail}}<div class="detail-meta"><span>{{.}}</span></div>{{end}}</div>
<h3>Findings</h3>{{if .Findings}}<div class="card"><table class="findings"><caption class="sr-only">Compatibility findings</caption><thead><tr><th>severity</th><th>rule</th><th>path</th><th>message</th></tr></thead><tbody>{{range .Findings}}<tr><td><span class="badge st-{{fclass .Class}}">{{.Class}}</span></td><td><code>{{.Rule}}</code></td><td><code>{{.Path}}</code></td><td>{{.Message}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="card"><p class="empty">{{if .Detail}}{{.Detail}}{{else}}No contract-relevant findings.{{end}}</p></div>{{end}}
{{if or .ConsumerRecord .ProviderRecord}}<details class="disclosure"><summary>Deploy provenance</summary><div class="provenance">{{with .ConsumerRecord}}<p class="prov">consumer version <code>{{.Label}}</code>{{if .Version}} · hash <code>{{.Hash}}</code>{{end}} · recorded <time datetime="{{.RecordedAt}}">{{shorttime .RecordedAt}}</time> by {{.RecordedBy}}</p>{{end}}{{with .ProviderRecord}}<p class="prov">provider version <code>{{.Label}}</code>{{if .Version}} · hash <code>{{.Hash}}</code>{{end}} · recorded <time datetime="{{.RecordedAt}}">{{shorttime .RecordedAt}}</time> by {{.RecordedBy}}</p>{{end}}</div></details>{{end}}
{{if or .ConsumerLines .ProviderLines}}<details class="disclosure"><summary>Compare schemas</summary><div class="bodies">
<div><h4>{{.ConsumerBodyLabel}}</h4>{{if .ConsumerLines}}<pre>{{range .ConsumerLines}}{{if .Class}}<span class="hl st-{{.Class}}">{{.Text}}</span>{{else}}{{.Text}}{{end}}
{{end}}</pre>{{else}}<p class="body-empty">not available</p>{{end}}</div>
<div><h4>{{.ProviderBodyLabel}}</h4>{{if .ProviderLines}}<pre>{{range .ProviderLines}}{{if .Class}}<span class="hl st-{{.Class}}">{{.Text}}</span>{{else}}{{.Text}}{{end}}
{{end}}</pre>{{else}}<p class="body-empty">not available</p>{{end}}</div>
</div>
</details>{{end}}</section>{{end}}
`))

type matrixStatusCount struct {
	Status matrixStatus
	N      int
}

type matrixHTMLModal struct {
	ID, Title, Scope, Check              string
	Consumer, Provider, Interaction      string
	ConsumerSlug, ContractSlug           string
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

// promoRow is one rendered promotion check; Check is empty for the
// per-service in-sync row.
type promoRow struct {
	promoEdge
	Check string
	Modal *matrixHTMLModal
}

type pipelineArrow struct {
	Status matrixStatus
	Title  string
}

// pipelineNode is one env in the health strip; Arrow is nil after the last
// pipeline env and on envs outside the pipeline.
type pipelineNode struct {
	Env, Summary string
	Status       matrixStatus
	Arrow        *pipelineArrow
}

// contractRef identifies one wire contract: the provider service plus the
// interaction it provides. Every matrix edge belongs to exactly one contract;
// promotion checks are matched via promoRef.
type contractRef struct{ Provider, Interaction string }

// contractEnvBadge is one per-env worst-status marker on a landing row.
type contractEnvBadge struct {
	Env    string
	Status matrixStatus
}

// provEnv is the provider's own deploy record for one env, from the
// inventory; fills environments-table rows no consumer edge reaches.
type provEnv struct {
	Env    string
	Record *deployRecord
}

// contractSummary is one landing-list row; Status is the worst across the
// contract's deployed edges and matched promotion checks.
type contractSummary struct {
	Provider, Interaction, Slug, ProviderSlug string
	Consumers                                 int
	Envs                                      []contractEnvBadge
	Status, HealthStatus, PromoStatus         matrixStatus
	HasPromotion                              bool
}

// contractEnvRow is one line of a contract's environments table: where the
// contract is deployed, how healthy it is there, and whether promoting that
// env into the next pipeline stage would keep it compatible. NextEnv is empty
// on the last pipeline env and on envs outside the pipeline.
type contractEnvRow struct {
	Env             string
	ProviderVersion *deployRecord
	Consumers       int
	Status          matrixStatus
	NextEnv         string
	PromoStatus     matrixStatus
	PromoDetail     string
}

// contractEnvSection is one env's consumer table on a contract view.
type contractEnvSection struct {
	Env  string
	Rows []matrixHTMLRow
}

// contractPromoSection is one adjacent env pair's promotion checks on a
// contract view; pairs without checks for the contract are omitted.
type contractPromoSection struct {
	Pair string // "dev → staging"
	Rows []promoRow
}

// contractView is one contract's page. Promotion gates the environments
// table's promotion column: without a pipeline there is nothing to promote
// into, so the column is omitted.
type contractView struct {
	contractSummary
	Promotion     bool
	EnvRows       []contractEnvRow
	EnvSections   []contractEnvSection
	PromoSections []contractPromoSection
}

type reportPageData struct {
	ContractLabel, ServiceLabel, EnvLabel, PromoLabel    string
	Verdict, VerdictTone                                 string
	Counts                                               []matrixStatusCount
	Envs                                                 []string
	Pipeline                                             []pipelineNode
	Contracts                                            []contractSummary
	Views                                                []contractView
	Services                                             []serviceSummary
	ServiceViews                                         []serviceView
	Details                                              []*matrixHTMLModal
	Attention                                            []attentionItem
	Failing, Warnings, TrackedContracts, PromotionIssues int
	HasPromotion                                         bool
	ContractFilters, ServiceFilters                      filterData
}

type filterData struct {
	Counts []matrixStatusCount
	Envs   []string
}

type attentionItem struct {
	Kind, Scope, Primary, Secondary, Detail, Href string
	Status                                        matrixStatus
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
// collide ("a b" vs "a-b"); contract slugs dedupe on top of this.
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

// matrixStatusOrder is worst first: the chip row, the verdict and every table
// lead with the action items.
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

func countsSummary(counts []matrixStatusCount) string {
	var parts []string
	for _, c := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", c.N, c.Status))
	}
	return strings.Join(parts, " · ")
}

// promoRef derives the contract a promotion check belongs to; ok is false for
// the per-service rows without an interaction (in-sync rollups, missing-blob
// errors). provides => Service is the provider; consumes => Counterpart is
// (see promoParties).
func promoRef(p promoEdge) (contractRef, bool) {
	if p.Interaction == "" {
		return contractRef{}, false
	}
	if p.Side == "provides" {
		return contractRef{p.Service, p.Interaction}, true
	}
	return contractRef{p.Counterpart, p.Interaction}, true
}

// claimSlug returns base, or base-N on collision, and records the claim.
func claimSlug(seen map[string]bool, base string) string {
	s := base
	for n := 2; seen[s]; n++ {
		s = fmt.Sprintf("%s-%d", base, n)
	}
	seen[s] = true
	return s
}

// contractSlugs assigns each ref a DOM-safe view id. anchorSlug can collide
// ("a-b/c" vs "a/b-c"), so collisions get a counter suffix; refs must arrive
// sorted for the numbering to be deterministic.
func contractSlugs(refs []contractRef) map[contractRef]string {
	out := make(map[contractRef]string, len(refs))
	seen := map[string]bool{}
	for _, r := range refs {
		out[r] = claimSlug(seen, "c-"+anchorSlug(r.Provider+"/"+r.Interaction))
	}
	return out
}

// serviceSlugs assigns each service a view id; names arrive sorted (NF3).
// Service names match ^[a-z0-9][a-z0-9-]*$ so the slug is effectively the
// name, but the dedupe guard mirrors contractSlugs anyway. The c-/s-/view-
// prefixes keep the id namespaces disjoint.
func serviceSlugs(names []string) map[string]string {
	out := make(map[string]string, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		out[name] = claimSlug(seen, "s-"+anchorSlug(name))
	}
	return out
}

// envLess orders envs pipeline-first (promotion order), then alphabetically.
func envLess(pipeline []string) func(a, b string) bool {
	rank := make(map[string]int, len(pipeline))
	for i, env := range pipeline {
		if _, exists := rank[env]; !exists {
			rank[env] = i
		}
	}
	return func(a, b string) bool {
		ra, aIn := rank[a]
		rb, bIn := rank[b]
		switch {
		case aIn && bIn:
			return ra < rb
		case aIn != bIn:
			return aIn
		default:
			return a < b
		}
	}
}

// nextEnvs maps each pipeline env to the one it promotes into.
func nextEnvs(pipeline []string) map[string]string {
	next := make(map[string]string, len(pipeline))
	for i := 0; i+1 < len(pipeline); i++ {
		next[pipeline[i]] = pipeline[i+1]
	}
	return next
}

// edgeModal builds the dedicated detail view for one deployed edge.
func edgeModal(id, contractSlug string, e matrixEdge) *matrixHTMLModal {
	hasBodies := e.ConsumerBody != nil && e.ProviderBody != nil
	m := &matrixHTMLModal{
		ID:    id,
		Title: fmt.Sprintf("%s → %s/%s", e.Consumer, e.Provider, e.Interaction), Scope: e.Env,
		Consumer: e.Consumer, Provider: e.Provider, Interaction: e.Interaction,
		ContractSlug:      contractSlug,
		ConsumerBodyLabel: "consumer projection", ProviderBodyLabel: "provider schema",
		Status: e.Status, Detail: e.Detail, Findings: e.Findings,
		ConsumerRecord: e.ConsumerRecord, ProviderRecord: e.ProviderRecord,
	}
	if hasBodies {
		marks := bodyMarks(e.Findings)
		m.ConsumerLines = bodySide(e.ConsumerBody, marks)
		m.ProviderLines = bodySide(e.ProviderBody, marks)
	}
	return m
}

// promoModal builds the dedicated detail view for one promotion row. The body
// labels distinguish the candidate from what is deployed in the target env.
func promoModal(id, contractSlug string, r promoRow) *matrixHTMLModal {
	consumer, provider := promoParties(r.promoEdge)
	m := &matrixHTMLModal{
		ID:    id,
		Title: fmt.Sprintf("%s → %s/%s", consumer, provider, r.Interaction),
		Scope: "promotion " + r.From + " → " + r.To, Check: r.Check,
		Consumer: consumer, Provider: provider, Interaction: r.Interaction,
		ContractSlug: contractSlug,
		Status:       r.Status, Detail: r.Detail, Findings: r.Findings,
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
	return m
}

// promoCell summarizes a contract's promotion checks for one env pair into the
// environments-table cell. When the service's checks collapsed into a
// per-service row (in sync, or a missing blob) the provider's row speaks for
// the contract.
func promoCell(matched, promos []promoEdge, provider, from, to string) (matrixStatus, string) {
	var statuses []matrixStatus
	for _, p := range matched {
		if p.From == from && p.To == to {
			statuses = append(statuses, p.Status)
		}
	}
	if len(statuses) > 0 {
		counts := matrixCounts(statuses)
		return counts[0].Status, countsSummary(counts)
	}
	for _, p := range promos {
		if p.Interaction == "" && p.Service == provider && p.From == from && p.To == to {
			return p.Status, p.Detail
		}
	}
	return matrixStatusUntracked, "no promotion checks"
}

// buildContractView assembles one contract's page: envs in pipeline order,
// rows worst-first within each table, modal ids numbered in that order. prov
// carries the provider's own deploy records so envs no consumer edge reaches
// still get an environments-table row (untracked: nothing was checked there).
func buildContractView(ref contractRef, slug string, edges []matrixEdge, promos []promoEdge,
	prov []provEnv, less func(a, b string) bool, next map[string]string) contractView {
	v := contractView{Promotion: len(next) > 0, contractSummary: contractSummary{
		Provider: ref.Provider, Interaction: ref.Interaction, Slug: slug,
		ProviderSlug: "s-" + anchorSlug(ref.Provider), Status: matrixStatusUntracked,
		HealthStatus: matrixStatusUntracked, PromoStatus: matrixStatusUntracked}}

	byEnv := map[string][]matrixEdge{}
	envSet := map[string]bool{}
	for _, e := range edges {
		if e.Provider == ref.Provider && e.Interaction == ref.Interaction {
			byEnv[e.Env] = append(byEnv[e.Env], e)
			envSet[e.Env] = true
		}
	}
	provRec := make(map[string]*deployRecord, len(prov))
	for _, p := range prov {
		provRec[p.Env] = p.Record
		envSet[p.Env] = true
	}
	envs := sortedKeys(envSet)
	sort.SliceStable(envs, func(i, j int) bool { return less(envs[i], envs[j]) })

	var matched []promoEdge
	for _, p := range promos {
		if r, ok := promoRef(p); ok && r == ref {
			matched = append(matched, p)
		}
	}

	consumers := map[string]bool{}
	var healthStatuses []matrixStatus
	en := 0
	for _, env := range envs {
		ces := byEnv[env]
		sort.SliceStable(ces, func(i, j int) bool {
			if ra, rb := matrixStatusRank(ces[i].Status), matrixStatusRank(ces[j].Status); ra != rb {
				return ra < rb
			}
			return ces[i].Consumer < ces[j].Consumer
		})
		row := contractEnvRow{Env: env, Status: matrixStatusUntracked, ProviderVersion: provRec[env]}
		if len(ces) > 0 {
			// Rows are worst-first, so the first row carries the env's status.
			row.Status = ces[0].Status
			sec := contractEnvSection{Env: env, Rows: make([]matrixHTMLRow, len(ces))}
			envConsumers := map[string]bool{}
			for i, e := range ces {
				r := matrixHTMLRow{matrixEdge: e}
				r.Modal = edgeModal(fmt.Sprintf("edge-%s-e%d", slug, en), slug, e)
				en++
				sec.Rows[i] = r
				envConsumers[e.Consumer] = true
				consumers[e.Consumer] = true
				healthStatuses = append(healthStatuses, e.Status)
				if row.ProviderVersion == nil {
					row.ProviderVersion = e.ProviderRecord
				}
			}
			row.Consumers = len(envConsumers)
			v.EnvSections = append(v.EnvSections, sec)
		}
		if to, ok := next[env]; ok {
			row.NextEnv = to
			row.PromoStatus, row.PromoDetail = promoCell(matched, promos, ref.Provider, env, to)
		}
		v.EnvRows = append(v.EnvRows, row)
		v.Envs = append(v.Envs, contractEnvBadge{Env: env, Status: row.Status})
	}
	for _, p := range matched {
		consumer, _ := promoParties(p)
		consumers[consumer] = true
	}
	v.Consumers = len(consumers)

	// Matched checks arrive pair-by-pair in pipeline order; one section each.
	pn := 0
	for start := 0; start < len(matched); {
		end := start
		for end < len(matched) && matched[end].From == matched[start].From && matched[end].To == matched[start].To {
			end++
		}
		rows := make([]promoRow, end-start)
		for i, p := range matched[start:end] {
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
			rows[i].Modal = promoModal(fmt.Sprintf("promotion-%s-p%d", slug, pn), slug, rows[i])
			pn++
		}
		v.PromoSections = append(v.PromoSections,
			contractPromoSection{Pair: matched[start].From + " → " + matched[start].To, Rows: rows})
		start = end
	}
	v.HealthStatus = worstStatus(healthStatuses)
	var promoStatuses []matrixStatus
	for _, p := range matched {
		promoStatuses = append(promoStatuses, p.Status)
	}
	v.HasPromotion = len(promoStatuses) > 0
	v.PromoStatus = worstStatus(promoStatuses)
	attention := append(append([]matrixStatus(nil), healthStatuses...), promoStatuses...)
	v.Status = worstStatus(attention)
	return v
}

// buildContracts regroups the matrix by contract: one landing summary and one
// view per (provider, interaction), worst contracts first. The inventory seeds
// contracts for provided interactions no edge or promotion check reaches.
func buildContracts(edges []matrixEdge, promos []promoEdge, pipeline []string,
	inv []invService) ([]contractSummary, []contractView) {
	set := map[contractRef]bool{}
	for _, e := range edges {
		set[contractRef{e.Provider, e.Interaction}] = true
	}
	for _, p := range promos {
		if r, ok := promoRef(p); ok {
			set[r] = true
		}
	}
	// One ref has one provider service, so each slice stays env-sorted (inv
	// is service-sorted with sorted envs).
	provIdx := map[contractRef][]provEnv{}
	for _, s := range inv {
		for _, e := range s.Envs {
			for _, item := range e.Provides {
				ref := contractRef{s.Service, item.Key}
				set[ref] = true
				provIdx[ref] = append(provIdx[ref], provEnv{Env: e.Env, Record: item.Record})
			}
		}
	}
	refs := make([]contractRef, 0, len(set))
	for r := range set {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Provider != refs[j].Provider {
			return refs[i].Provider < refs[j].Provider
		}
		return refs[i].Interaction < refs[j].Interaction
	})
	slugs := contractSlugs(refs)

	less := envLess(pipeline)
	next := nextEnvs(pipeline)
	views := make([]contractView, 0, len(refs))
	for _, ref := range refs {
		views = append(views, buildContractView(ref, slugs[ref], edges, promos, provIdx[ref], less, next))
	}
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].contractSummary, views[j].contractSummary
		if ra, rb := matrixStatusRank(a.Status), matrixStatusRank(b.Status); ra != rb {
			return ra < rb
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Interaction < b.Interaction
	})
	summaries := make([]contractSummary, len(views))
	for i, v := range views {
		summaries[i] = v.contractSummary
	}
	return summaries, views
}

// serviceSummary is one Services-tab row; Status is the worst across the
// per-env badges. Role is provider, consumer, or both.
type serviceSummary struct {
	Service, Slug, Role string
	Provides, Consumes  int // distinct interactions / consumed contracts
	Envs                []contractEnvBadge
	Status              matrixStatus
}

// serviceEnvRow is one line of a service's environments table.
type serviceEnvRow struct {
	Env, RecordedAt, RecordedBy string
	Provides, Consumes          int
	Status                      matrixStatus
	Stale                       bool
}

// svcItemEnv is one env badge on a provides/consumes row: the service's
// pinned version there and the health of that contract's edges in the env.
type svcItemEnv struct {
	Env, Version, Title, DetailSlug string // Title: the version cell's provenance tooltip
	Status                          matrixStatus
}

// svcItemRow is one provided interaction or consumed contract; it links to
// the contract's own view instead of duplicating its modals.
type svcItemRow struct {
	Label, ContractSlug string
	Envs                []svcItemEnv
}

// serviceView is one service's page.
type serviceView struct {
	serviceSummary
	EnvRows            []serviceEnvRow
	ProvideRows        []svcItemRow
	ConsumeRows        []svcItemRow
	DeployedIssues     []serviceAttention
	PromotionGroups    []serviceAttentionGroup
	HasPromotionChecks bool
}

type serviceAttention struct {
	Scope, Role, Relationship, Detail, DetailSlug string
	Status                                        matrixStatus
}

type serviceAttentionGroup struct {
	Label, From, To, Outcome, Summary string
	Status                            matrixStatus
	Rows                              []serviceAttention
}

func promotionGroupOutcome(statuses []matrixStatus, issues int) (matrixStatus, string, string) {
	counts := map[matrixStatus]int{}
	for _, status := range statuses {
		counts[status]++
	}
	switch {
	case counts[matrixStatusIncompatible] > 0:
		return matrixStatusIncompatible, "Blocked", plural(issues, "issue") + " requiring attention"
	case counts[matrixStatusError] > 0:
		return matrixStatusError, "Blocked", plural(issues, "issue") + " requiring attention"
	case counts[matrixStatusUntracked] > 0:
		return matrixStatusUntracked, "Unverified", fmt.Sprintf("%d of %s could not be verified",
			counts[matrixStatusUntracked], plural(len(statuses), "check"))
	case counts[matrixStatusWarning] > 0:
		return matrixStatusWarning, "Ready with warnings", "Compatible with " + plural(counts[matrixStatusWarning], "warning")
	default:
		passed := plural(len(statuses), "compatibility check")
		if len(statuses) > 1 {
			passed = "All " + passed
		}
		return matrixStatusOK, "Ready", passed + " passed"
	}
}

// worstStatus is the worst of the collected statuses; untracked when nothing
// was checked at all (never silently green, but an ok check must beat the
// no-checks sentinel, so the sentinel only applies to the empty case).
func worstStatus(statuses []matrixStatus) matrixStatus {
	if len(statuses) == 0 {
		return matrixStatusUntracked
	}
	return matrixCounts(statuses)[0].Status
}

// svcEnvStatus is the worst edge status touching a service in one env.
func svcEnvStatus(edges []matrixEdge, svc, env string) matrixStatus {
	var statuses []matrixStatus
	for _, e := range edges {
		if e.Env == env && (e.Consumer == svc || e.Provider == svc) {
			statuses = append(statuses, e.Status)
		}
	}
	return worstStatus(statuses)
}

// contractEnvStatus is the worst edge status of one contract in one env.
func contractEnvStatus(edges []matrixEdge, ref contractRef, env string) matrixStatus {
	var statuses []matrixStatus
	for _, e := range edges {
		if e.Env == env && e.Provider == ref.Provider && e.Interaction == ref.Interaction {
			statuses = append(statuses, e.Status)
		}
	}
	return worstStatus(statuses)
}

type edgeRef struct{ Env, Consumer, Provider, Interaction string }

func consumerEnvStatus(edges []matrixEdge, consumer string, ref contractRef, env string) matrixStatus {
	for _, e := range edges {
		if e.Env == env && e.Consumer == consumer && e.Provider == ref.Provider && e.Interaction == ref.Interaction {
			return e.Status
		}
	}
	return matrixStatusUntracked
}

// recTitle is the provenance tooltip shared by version cells.
func recTitle(r *deployRecord) string {
	return r.Hash + " · recorded " + r.RecordedAt + " by " + r.RecordedBy
}

func buildServiceAttention(svc string, edges []matrixEdge, promos []promoEdge,
	detailOf map[edgeRef]string, promoDetailOf map[promoDetailRef]string,
	less func(a, b string) bool) ([]serviceAttention, []serviceAttentionGroup, bool) {
	var deployed []serviceAttention
	for _, e := range edges {
		if e.Status == matrixStatusOK || (e.Consumer != svc && e.Provider != svc) {
			continue
		}
		role := "provider"
		if e.Consumer == svc {
			role = "consumer"
		}
		deployed = append(deployed, serviceAttention{Scope: e.Env, Role: role,
			Relationship: fmt.Sprintf("%s → %s/%s", e.Consumer, e.Provider, e.Interaction),
			Status:       e.Status, Detail: e.Detail,
			DetailSlug: detailOf[edgeRef{e.Env, e.Consumer, e.Provider, e.Interaction}]})
	}
	type transition struct{ from, to string }
	promotionByTransition := map[transition][]serviceAttention{}
	promotionStatuses := map[transition][]matrixStatus{}
	hasPromotionChecks := false
	for _, p := range promos {
		if p.Service != svc {
			continue
		}
		hasPromotionChecks = true
		pair := transition{p.From, p.To}
		promotionStatuses[pair] = append(promotionStatuses[pair], p.Status)
		if p.Status == matrixStatusOK {
			continue
		}
		role := p.Side
		relationship := p.Service
		detailSlug := ""
		if p.Interaction != "" {
			consumer, provider := promoParties(p)
			switch role {
			case "provides":
				role = "provider"
			case "consumes":
				role = "consumer"
			}
			relationship = fmt.Sprintf("%s → %s/%s", consumer, provider, p.Interaction)
			detailSlug = promoDetailOf[promoDetailRef{p.From, p.To, p.Service, p.Side,
				p.Counterpart, p.Interaction}]
		} else {
			role = "service"
		}
		promotionByTransition[pair] = append(promotionByTransition[pair], serviceAttention{
			Role: role, Relationship: relationship, Status: p.Status, Detail: p.Detail,
			DetailSlug: detailSlug})
	}
	sortRows := func(rows []serviceAttention) {
		sort.SliceStable(rows, func(i, j int) bool {
			if a, b := matrixStatusRank(rows[i].Status), matrixStatusRank(rows[j].Status); a != b {
				return a < b
			}
			if rows[i].Scope != rows[j].Scope {
				return less(rows[i].Scope, rows[j].Scope)
			}
			if rows[i].Role != rows[j].Role {
				return rows[i].Role < rows[j].Role
			}
			return rows[i].Relationship < rows[j].Relationship
		})
	}
	sortRows(deployed)
	pairs := make([]transition, 0, len(promotionStatuses))
	for pair := range promotionStatuses {
		pairs = append(pairs, pair)
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].from != pairs[j].from {
			return less(pairs[i].from, pairs[j].from)
		}
		return less(pairs[i].to, pairs[j].to)
	})
	promotion := make([]serviceAttentionGroup, 0, len(pairs))
	for _, pair := range pairs {
		rows := promotionByTransition[pair]
		sortRows(rows)
		status, outcome, summary := promotionGroupOutcome(promotionStatuses[pair], len(rows))
		promotion = append(promotion, serviceAttentionGroup{
			Label: pair.from + " → " + pair.to, From: pair.from, To: pair.to,
			Status: status, Outcome: outcome, Summary: summary, Rows: rows})
	}
	return deployed, promotion, hasPromotionChecks
}

// buildServices turns the inventory into the Services tab and one view per
// service, worst-first then by name. slugOf maps every contract to its view
// id (complete after buildContracts, which seeds refs from the inventory).
func buildServices(inv []invService, edges []matrixEdge, promos []promoEdge, slugOf map[contractRef]string,
	detailOf map[edgeRef]string, promoDetailOf map[promoDetailRef]string,
	less func(a, b string) bool) ([]serviceSummary, []serviceView) {
	names := make([]string, len(inv))
	for i, s := range inv {
		names[i] = s.Service
	}
	// Slugs claim on name order, before the display sort (deterministic).
	slugs := serviceSlugs(names)

	views := make([]serviceView, 0, len(inv))
	for _, s := range inv {
		v := serviceView{serviceSummary: serviceSummary{
			Service: s.Service, Slug: slugs[s.Service], Status: matrixStatusUntracked}}
		envs := append([]invEnv(nil), s.Envs...)
		sort.SliceStable(envs, func(i, j int) bool { return less(envs[i].Env, envs[j].Env) })

		provides, consumes := map[string]bool{}, map[string]bool{}
		var statuses []matrixStatus
		for _, e := range envs {
			row := serviceEnvRow{Env: e.Env, RecordedAt: e.RecordedAt, RecordedBy: e.RecordedBy,
				Provides: len(e.Provides), Consumes: len(e.Consumes), Stale: e.Stale,
				Status: svcEnvStatus(edges, s.Service, e.Env)}
			for _, item := range e.Provides {
				provides[item.Key] = true
			}
			for _, item := range e.Consumes {
				consumes[item.Key] = true
			}
			statuses = append(statuses, row.Status)
			v.EnvRows = append(v.EnvRows, row)
			v.Envs = append(v.Envs, contractEnvBadge{Env: e.Env, Status: row.Status})
		}
		item := func(key string, ref contractRef, consumer string, rec func(invEnv) *deployRecord) svcItemRow {
			r := svcItemRow{Label: key, ContractSlug: slugOf[ref]}
			for _, e := range envs {
				if record := rec(e); record != nil {
					status := contractEnvStatus(edges, ref, e.Env)
					var detailSlug string
					if consumer != "" {
						status = consumerEnvStatus(edges, consumer, ref, e.Env)
						detailSlug = detailOf[edgeRef{e.Env, consumer, ref.Provider, ref.Interaction}]
					}
					r.Envs = append(r.Envs, svcItemEnv{Env: e.Env, Version: record.Label(),
						Title: recTitle(record), Status: status, DetailSlug: detailSlug})
				}
			}
			return r
		}
		for _, id := range sortedKeys(provides) {
			v.ProvideRows = append(v.ProvideRows, item(id, contractRef{s.Service, id}, "",
				func(e invEnv) *deployRecord { return invRecord(e.Provides, id) }))
		}
		for _, key := range sortedKeys(consumes) {
			provider, id, _ := cutString(key, "/")
			v.ConsumeRows = append(v.ConsumeRows, item(key, contractRef{provider, id}, s.Service,
				func(e invEnv) *deployRecord { return invRecord(e.Consumes, key) }))
		}
		v.Provides, v.Consumes = len(provides), len(consumes)
		switch {
		case len(provides) > 0 && len(consumes) > 0:
			v.Role = "both"
		case len(provides) > 0:
			v.Role = "provider"
		default:
			v.Role = "consumer"
		}
		if len(statuses) > 0 {
			v.Status = matrixCounts(statuses)[0].Status
		}
		v.DeployedIssues, v.PromotionGroups, v.HasPromotionChecks = buildServiceAttention(
			s.Service, edges, promos, detailOf, promoDetailOf, less)
		views = append(views, v)
	}
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].serviceSummary, views[j].serviceSummary
		if ra, rb := matrixStatusRank(a.Status), matrixStatusRank(b.Status); ra != rb {
			return ra < rb
		}
		return a.Service < b.Service
	})
	summaries := make([]serviceSummary, len(views))
	for i, v := range views {
		summaries[i] = v.serviceSummary
	}
	return summaries, views
}

// invRecord finds one key's record in a Key-sorted item list; nil when the
// env does not pin it.
func invRecord(items []invItem, key string) *deployRecord {
	for _, it := range items {
		if it.Key == key {
			return it.Record
		}
	}
	return nil
}

// buildPipeline lays out the health strip: pipeline envs in promotion order
// with an arrow per adjacent pair, then any deploy-recorded envs outside the
// pipeline. Nil when there is neither a pipeline nor more than one env.
func buildPipeline(pipeline []string, edges []matrixEdge, promos []promoEdge) []pipelineNode {
	byEnv := map[string][]matrixStatus{}
	for _, e := range edges {
		byEnv[e.Env] = append(byEnv[e.Env], e.Status)
	}
	if len(pipeline) == 0 && len(byEnv) < 2 {
		return nil
	}
	node := func(env string) pipelineNode {
		n := pipelineNode{Env: env, Status: matrixStatusUntracked, Summary: "no deploy records"}
		if statuses := byEnv[env]; len(statuses) > 0 {
			counts := matrixCounts(statuses)
			n.Status, n.Summary = counts[0].Status, countsSummary(counts)
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
	var rest []string
	for env := range byEnv {
		if !inPipe[env] {
			rest = append(rest, env)
		}
	}
	sort.Strings(rest)
	for _, env := range rest {
		nodes = append(nodes, node(env))
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

type promoDetailRef struct{ From, To, Service, Side, Counterpart, Interaction string }

func collectDetails(views []contractView) ([]*matrixHTMLModal, map[edgeRef]string, map[promoDetailRef]string) {
	var details []*matrixHTMLModal
	edges := map[edgeRef]string{}
	promos := map[promoDetailRef]string{}
	for _, v := range views {
		for _, section := range v.EnvSections {
			for _, row := range section.Rows {
				if row.Modal == nil {
					continue
				}
				details = append(details, row.Modal)
				edges[edgeRef{row.Env, row.Consumer, row.Provider, row.Interaction}] = row.Modal.ID
			}
		}
		for _, section := range v.PromoSections {
			for _, row := range section.Rows {
				if row.Modal == nil {
					continue
				}
				details = append(details, row.Modal)
				promos[promoDetailRef{row.From, row.To, row.Service, row.Side,
					row.Counterpart, row.Interaction}] = row.Modal.ID
			}
		}
	}
	return details, edges, promos
}

func buildAttention(edges []matrixEdge, promos []promoEdge, edgeDetails map[edgeRef]string,
	promoDetails map[promoDetailRef]string, serviceSlugs map[string]string) []attentionItem {
	var out []attentionItem
	for _, e := range edges {
		if e.Status == matrixStatusOK {
			continue
		}
		out = append(out, attentionItem{Kind: "deployed edge", Scope: e.Env,
			Primary: fmt.Sprintf("%s → %s/%s", e.Consumer, e.Provider, e.Interaction),
			Status:  e.Status, Detail: e.Detail,
			Href: edgeDetails[edgeRef{e.Env, e.Consumer, e.Provider, e.Interaction}]})
	}
	for _, p := range promos {
		if p.Status == matrixStatusOK {
			continue
		}
		kind := "promotion"
		if p.InSync {
			kind = "target health"
		}
		primary := p.Service
		href := serviceSlugs[p.Service]
		if p.Interaction != "" {
			consumer, provider := promoParties(p)
			primary = fmt.Sprintf("%s → %s/%s", consumer, provider, p.Interaction)
			href = promoDetails[promoDetailRef{p.From, p.To, p.Service, p.Side,
				p.Counterpart, p.Interaction}]
		}
		out = append(out, attentionItem{Kind: kind, Scope: p.From + " → " + p.To,
			Primary: primary, Secondary: promoCheck(p), Status: p.Status, Detail: p.Detail, Href: href})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := matrixStatusRank(out[i].Status), matrixStatusRank(out[j].Status); a != b {
			return a < b
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Primary < out[j].Primary
	})
	return out
}

// renderMatrixHTML renders the matrix as a self-contained HTML page
// (`matrix --format html` / `-o *.html`): an operational overview, contract and
// service directories, and dedicated relationship detail views.
func renderMatrixHTML(edges []matrixEdge, promos []promoEdge, pipeline []string, inv []invService) []byte {
	contracts, views := buildContracts(edges, promos, pipeline, inv)
	d := reportPageData{Contracts: contracts, Views: views,
		ContractLabel: plural(len(contracts), "contract"), HasPromotion: len(promos) > 0}
	slugOf := make(map[contractRef]string, len(contracts))
	statuses := make([]matrixStatus, len(contracts))
	for i, c := range contracts {
		statuses[i] = c.Status
		slugOf[contractRef{c.Provider, c.Interaction}] = c.Slug
	}
	d.Counts = matrixCounts(statuses)
	details, edgeDetails, promoDetails := collectDetails(views)
	d.Details = details
	d.Services, d.ServiceViews = buildServices(inv, edges, promos, slugOf, edgeDetails,
		promoDetails, envLess(pipeline))
	serviceSlugs := make(map[string]string, len(d.Services))
	for _, s := range d.Services {
		serviceSlugs[s.Service] = s.Slug
	}
	serviceStatuses := make([]matrixStatus, len(d.Services))
	for i, s := range d.Services {
		serviceStatuses[i] = s.Status
	}
	for i := range d.Views {
		if slug := serviceSlugs[d.Views[i].Provider]; slug != "" {
			d.Views[i].ProviderSlug = slug
		}
	}
	for _, detail := range d.Details {
		if slug := serviceSlugs[detail.Consumer]; slug != "" {
			detail.ConsumerSlug = slug
		}
	}
	d.Attention = buildAttention(edges, promos, edgeDetails, promoDetails, serviceSlugs)
	d.ServiceLabel = plural(len(d.Services), "service")
	envs := map[string]bool{}
	for _, e := range edges {
		envs[e.Env] = true
	}
	for _, s := range inv {
		for _, e := range s.Envs {
			envs[e.Env] = true
		}
	}
	d.Envs = sortedKeys(envs)
	less := envLess(pipeline)
	sort.SliceStable(d.Envs, func(i, j int) bool { return less(d.Envs[i], d.Envs[j]) })
	d.EnvLabel = plural(len(d.Envs), "env")
	d.ContractFilters = filterData{Counts: d.Counts, Envs: d.Envs}
	d.ServiceFilters = filterData{Counts: matrixCounts(serviceStatuses), Envs: d.Envs}
	if len(promos) > 0 {
		d.PromoLabel = plural(len(promos), "promotion check")
	}
	d.Pipeline = buildPipeline(pipeline, edges, promos)

	failSet := map[string]bool{}
	for _, e := range edges {
		switch e.Status {
		case matrixStatusIncompatible, matrixStatusError:
			d.Failing++
			failSet[e.Env] = true
		case matrixStatusWarning:
			d.Warnings++
		}
	}
	for _, c := range contracts {
		if c.HealthStatus != matrixStatusUntracked {
			d.TrackedContracts++
		}
	}
	for _, p := range promos {
		if p.Status != matrixStatusOK {
			d.PromotionIssues++
		}
	}
	failEnvs := sortedKeys(failSet)
	sort.Slice(failEnvs, func(i, j int) bool { return less(failEnvs[i], failEnvs[j]) })
	switch {
	case d.Failing > 0:
		d.Verdict = fmt.Sprintf("%s in %s", plural(d.Failing, "failing edge"), strings.Join(failEnvs, ", "))
		d.VerdictTone = "bad"
	case d.Warnings > 0:
		d.Verdict = fmt.Sprintf("no failing edges · %s", plural(d.Warnings, "warning"))
		d.VerdictTone = "warn"
	case len(edges) > 0:
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
