// Package modal renders nui/neotree-style popup boxes for terminal UIs:
// rounded borders with the title embedded in the top border and a hint
// footer embedded in the bottom border. It only produces strings — the
// caller decides where to composite them.
package modal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Opts struct {
	Width  int    // total box width, borders included
	Title  string // embedded in the top border
	Footer string // embedded in the bottom border
	Accent lipgloss.TerminalColor
}

// Input renders a single-line text input popup with a prompt prefix.
func Input(o Opts, value string) string {
	accent := lipgloss.NewStyle().Foreground(o.Accent)
	return box(o, []string{accent.Bold(true).Render("> ") + value + accent.Render("▎")})
}

// Confirm renders a question popup; pair it with a y/n footer.
func Confirm(o Opts, message string) string {
	return box(o, wrap(message, o.Width-4))
}

// Notice renders a plain message popup (progress, errors).
func Notice(o Opts, message string) string {
	return box(o, wrap(message, o.Width-4))
}

// Lines renders pre-formatted lines verbatim (no wrapping) — for tables,
// key references, and other layouts the caller controls.
func Lines(o Opts, lines []string) string {
	return box(o, lines)
}

func box(o Opts, content []string) string {
	w := o.Width
	if w < 16 {
		w = 16
	}
	inner := w - 2

	line := lipgloss.NewStyle().Foreground(o.Accent)
	title := lipgloss.NewStyle().Foreground(o.Accent).Bold(true)
	footer := lipgloss.NewStyle().Foreground(o.Accent).Faint(true)

	var b strings.Builder
	b.WriteString(line.Render("╭") + embed(o.Title, inner, title, line) + line.Render("╮") + "\n")
	for _, c := range content {
		pad := inner - 2 - lipgloss.Width(c)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(line.Render("│") + " " + c + strings.Repeat(" ", pad) + " " + line.Render("│") + "\n")
	}
	b.WriteString(line.Render("╰") + embed(o.Footer, inner, footer, line) + line.Render("╯"))
	return b.String()
}

// embed renders a horizontal border line of exactly `inner` cells with an
// optional label woven in: ─ Label ────────
func embed(label string, inner int, labelStyle, lineStyle lipgloss.Style) string {
	if label == "" {
		return lineStyle.Render(strings.Repeat("─", inner))
	}
	text := " " + label + " "
	if max := inner - 2; lipgloss.Width(text) > max {
		rs := []rune(text)
		if max > 1 {
			text = string(rs[:max-1]) + "… "
		} else {
			text = ""
		}
	}
	fill := inner - 1 - lipgloss.Width(text)
	if fill < 0 {
		fill = 0
	}
	return lineStyle.Render("─") + labelStyle.Render(text) + lineStyle.Render(strings.Repeat("─", fill))
}

// wrap greedily word-wraps plain text to the given cell width.
func wrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= width:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
