package main

// Terminal renderer for diff/compat/check text output: one status row per
// interaction, findings indented beneath, a single closing verdict line.
// Everything colored goes through paint so non-TTY output (CI logs, pipes)
// stays plain.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wirefit/wirefit/internal/diff"
)

// colorEnabled: ANSI colors only when stdout is a terminal and neither
// NO_COLOR (https://no-color.org) nor TERM=dumb forbids them.
var colorEnabled = func() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

const (
	sgrRed    = "31;1"
	sgrGreen  = "32;1"
	sgrYellow = "33;1"
	sgrDim    = "2"
)

func paint(sgr, s string) string {
	if !colorEnabled {
		return s
	}
	return "\x1b[" + sgr + "m" + s + "\x1b[0m"
}

// glyph is the single-cell severity marker. Deliberately not the emoji
// variants: those render double-width and break column alignment.
func glyph(c diff.Class) string {
	switch c {
	case diff.Breaking:
		return paint(sgrRed, "✗")
	case diff.Warning:
		return paint(sgrYellow, "⚠")
	case diff.Safe:
		return paint(sgrGreen, "✓")
	}
	return paint(sgrDim, "·")
}

func dirLabel(d diff.Direction) string {
	switch d {
	case diff.P2C:
		return "provider → consumer"
	case diff.C2P:
		return "consumer → provider"
	}
	return string(d)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// countsLabel summarizes findings by severity: "2 breaking, 1 warning".
func countsLabel(fs []diff.Finding) string {
	var n [diff.Breaking + 1]int
	for _, f := range fs {
		n[f.Class]++
	}
	var parts []string
	if n[diff.Breaking] > 0 {
		parts = append(parts, fmt.Sprintf("%d breaking", n[diff.Breaking]))
	}
	if n[diff.Warning] > 0 {
		parts = append(parts, plural(n[diff.Warning], "warning"))
	}
	if n[diff.Safe] > 0 {
		parts = append(parts, fmt.Sprintf("%d safe", n[diff.Safe]))
	}
	if n[diff.Neutral] > 0 {
		parts = append(parts, fmt.Sprintf("%d neutral", n[diff.Neutral]))
	}
	return strings.Join(parts, ", ")
}

// wrap breaks s into lines at most width runes wide, on word boundaries.
// We don't probe the terminal; a conservative fixed width keeps continuation
// indentation intact instead of hard-wrapping at column 0.
func wrap(s string, width int) []string {
	if width < 24 {
		width = 24
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := []string{words[0]}
	for _, w := range words[1:] {
		last := lines[len(lines)-1]
		if len(last)+1+len(w) > width {
			lines = append(lines, w)
			continue
		}
		lines[len(lines)-1] = last + " " + w
	}
	return lines
}

// printFinding renders one finding: severity glyph, path and rule on the
// head line, the message wrapped underneath.
func printFinding(f diff.Finding, indent string) {
	head := indent + glyph(f.Class) + " " + f.Path + "  " + paint(sgrDim, f.Rule)
	if f.Overridden {
		head += " " + paint(sgrDim, "(overridden)")
	}
	fmt.Println(head)
	for _, line := range wrap(f.Message, 84-len(indent)) {
		fmt.Println(indent + "    " + line)
	}
	if len(f.ConsumedBy) > 0 {
		fmt.Println(indent + "    " + paint(sgrDim, "consumed by: "+strings.Join(f.ConsumedBy, ", ")))
	}
}

// verdictLine is the single closing line; breaking mirrors the exit code.
func verdictLine(breaking bool, counts string) string {
	verdict, suffix := paint(sgrGreen, "COMPATIBLE"), ""
	if breaking {
		verdict, suffix = paint(sgrRed, "BREAKING"), ", do not merge"
	}
	if counts != "" {
		suffix += " (" + counts + ")"
	}
	return "result: " + verdict + suffix
}

const coldStartNote = "cold start: no consumers registered, breaking downgraded to warning"

func printResult(r *diff.Result, format string) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		return
	}
	if len(r.Findings) == 0 {
		fmt.Println("no contract-relevant changes")
		return
	}
	head := plural(len(r.Findings), "finding")
	if lbl := dirLabel(r.Direction); lbl != "" {
		head += " (" + lbl + ")"
	}
	fmt.Println(head)
	if r.ColdStart {
		fmt.Println("  " + paint(sgrDim, coldStartNote))
	}
	for _, f := range r.Findings {
		printFinding(f, "  ")
	}
	fmt.Println(verdictLine(r.Max() == diff.Breaking, countsLabel(r.Findings)))
}

// matrixGlyph maps deployment statuses onto the shared severity glyphs.
func matrixGlyph(status matrixStatus) string {
	switch status {
	case matrixStatusOK:
		return glyph(diff.Safe)
	case matrixStatusWarning:
		return glyph(diff.Warning)
	case matrixStatusIncompatible, matrixStatusError:
		return glyph(diff.Breaking)
	}
	return paint(sgrDim, "·")
}

// printMatrixTerm renders the deployed matrix for the terminal: one row per
// consumer→provider edge, detail wrapped dim underneath.
func printMatrixTerm(edges []matrixEdge) {
	envs := map[string]bool{}
	for _, e := range edges {
		envs[e.Env] = true
	}
	fmt.Printf("wirefit matrix · %s · %s\n\n", plural(len(envs), "env"), plural(len(edges), "edge"))
	if len(edges) == 0 {
		fmt.Println("no deploy records; run `wirefit record-deploy` in each service first")
		return
	}
	envW, edgeW := 0, 0
	rows := make([]string, len(edges))
	for i, e := range edges {
		rows[i] = e.Consumer + " → " + e.Provider + "/" + e.Interaction
		envW = max(envW, len(e.Env))
		edgeW = max(edgeW, len([]rune(rows[i])))
	}
	for i, e := range edges {
		// pad by runes: the arrow in rows is multibyte, Printf pads by bytes
		pad := strings.Repeat(" ", edgeW-len([]rune(rows[i])))
		fmt.Printf("  %-*s  %s %s%s  %s\n", envW, e.Env, matrixGlyph(e.Status), rows[i], pad, e.Status)
		if e.Detail != "" {
			for _, line := range wrap(e.Detail, 76) {
				fmt.Println(strings.Repeat(" ", envW+8) + paint(sgrDim, line))
			}
		}
	}
}

// printCheck renders the check dashboard: one row per interaction carrying
// its worst severity and finding counts, details indented beneath.
func printCheck(service string, results map[string]*diff.Result, worst int) {
	keys := sortedResultKeys(results)
	interactions, nameW := 0, 0
	for _, k := range keys {
		if k != "overrides" {
			interactions++
		}
		if _, name, ok := strings.Cut(k, " "); ok && len(name) > nameW {
			nameW = len(name)
		}
	}
	fmt.Printf("wirefit check · %s · %s\n\n", service, plural(interactions, "interaction"))
	var all []diff.Finding
	for _, k := range keys {
		r := results[k]
		kind, name, _ := strings.Cut(k, " ")
		g, status := paint(sgrGreen, "✓"), paint(sgrDim, "no changes")
		if len(r.Findings) > 0 {
			g, status = glyph(r.Max()), countsLabel(r.Findings)
		}
		fmt.Printf("  %s %-9s %-*s  %s\n", g, kind, nameW, name, status)
		if r.ColdStart {
			fmt.Println("        " + paint(sgrDim, coldStartNote))
		}
		for _, f := range r.Findings {
			printFinding(f, "        ")
		}
		all = append(all, r.Findings...)
	}
	fmt.Println()
	fmt.Println(verdictLine(worst != 0, countsLabel(all)))
}
