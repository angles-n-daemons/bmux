package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Nerd-font glyphs per foreground command; Claude panes are detected via
// their title marker instead and get ✳ in Anthropic clay.
var commandIcons = map[string]string{
	"zsh": "", "bash": "", "sh": "", "fish": "", "dash": "", "ksh": "",
	"nvim": "", "vim": "", "vi": "",
	"node": "",
	"python": "", "python3": "", "ipython": "",
	"go": "",
	"git": "", "tig": "", "lazygit": "",
	"docker": "",
	"ssh": "", "mosh-client": "",
	"cargo": "", "rustc": "",
	"make": "", "cmake": "",
	"htop": "", "top": "", "btop": "",
	"man": "", "less": "", "bat": "",
}

const defaultIcon = ""

var styleClaude = lipgloss.NewStyle().Foreground(lipgloss.Color("#d97757"))

// paneGlyph returns the icon (plain glyph), display name, and whether this
// is a Claude pane (for styling).
func paneGlyph(p Pane) (icon, name string, isClaude bool) {
	if title, ok := claudeTitleName(p.Title); ok && !shellCommands[p.Command] {
		name = title
		if name == "" || name == "Claude Code" {
			name = "claude"
		}
		return "✳", name, true
	}
	if ic, ok := commandIcons[p.Command]; ok {
		return ic, p.Command, false
	}
	return defaultIcon, p.Command, false
}

var (
	styleRepo    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleCurrent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleLive    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleCursor  = lipgloss.NewStyle().Reverse(true)
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleStopped = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTitle   = lipgloss.NewStyle().Bold(true)
	styleFooter  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stylePrompt  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func agentBadge(agents []agentStatus) string {
	if len(agents) == 0 {
		return ""
	}
	counts := map[agentStatus]int{}
	for _, a := range agents {
		counts[a]++
	}
	var parts []string
	if n := counts[agentRunning]; n > 0 {
		parts = append(parts, styleRunning.Render(fmt.Sprintf("▶%d", n)))
	}
	if n := counts[agentWaiting]; n > 0 {
		parts = append(parts, styleWaiting.Render(fmt.Sprintf("⏸%d", n)))
	}
	if n := counts[agentStopped]; n > 0 {
		parts = append(parts, styleStopped.Render(fmt.Sprintf("⏹%d", n)))
	}
	return " " + strings.Join(parts, " ")
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

func (m model) foldMarker(r row) string {
	if !r.expandable() {
		return " "
	}
	if m.isExpanded(r) {
		return "" // cod chevron-down
	}
	return "" // cod chevron-right
}

// activePaneOf finds the active pane of a window, for its icon.
func (m model) activePaneOf(session string, window int) *Pane {
	for _, p := range m.snap.Panes[session] {
		if p.Window == window && p.Active {
			return &p
		}
	}
	return nil
}

// renderRow builds one display line. Selected rows are rendered plain and
// reversed so the cursor is readable regardless of inner styling.
func (m model) renderRow(r row, selected bool) string {
	indent := strings.Repeat("  ", r.Depth)
	width := m.width

	plain := func(parts ...string) string { return indent + strings.Join(parts, "") }

	switch r.Kind {
	case rowRepo:
		icon := "" // closed folder
		if m.isExpanded(r) {
			icon = "" // open folder
		}
		line := plain(icon, " ", r.Repo.Name)
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		return styleRepo.Render(truncate(line, width))

	case rowWorktree, rowSession:
		var name, branch, sessName string
		var agents []agentStatus
		live := false
		if r.Kind == rowWorktree {
			name = filepath.Base(r.WT.WT.Path)
			branch = r.WT.WT.Branch
			if r.WT.Session != nil {
				live = true
				agents = r.WT.Agents
				if r.WT.Session.Name != name {
					sessName = r.WT.Session.Name
				}
			}
		} else {
			name = r.Sess.Session.Name
			live = true
			agents = r.Sess.Agents
		}

		marker := "○"
		if live {
			marker = "●"
		}
		head := plain(m.foldMarker(r), marker, " ", name)
		if sessName != "" {
			head += " [" + sessName + "]"
		}
		badge := agentBadge(agents)

		if selected {
			line := head
			if branch != "" {
				line += "  " + branch
			}
			return styleCursor.Render(truncate(line, width))
		}

		// styled: name emphasized when it's the current session
		nameStyled := name
		current := r.sessionName() != "" && r.sessionName() == m.snap.CurrentSession
		if current {
			nameStyled = styleCurrent.Render(name)
		}
		markerStyled := styleDim.Render(marker)
		if live {
			markerStyled = styleLive.Render(marker)
		}
		out := indent + m.foldMarker(r) + markerStyled + " " + nameStyled
		if sessName != "" {
			out += styleDim.Render(" [" + sessName + "]")
		}
		out += badge
		if branch != "" {
			used := lipgloss.Width(out)
			if room := width - used - 2; room > 3 {
				out += styleDim.Render("  " + truncate(branch, room))
			}
		}
		return out

	case rowWindow:
		mark := " "
		if r.Win.Active {
			mark = "*"
		}
		icon, isClaude := defaultIcon, false
		name := r.Win.Name
		if ap := m.activePaneOf(r.Win.Session, r.Win.Index); ap != nil {
			icon, _, isClaude = paneGlyph(*ap)
		}
		line := plain(m.foldMarker(r), " ", fmt.Sprintf("%d: %s %s%s", r.Win.Index, icon, name, mark))
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		if isClaude {
			prefix := indent + m.foldMarker(r) + fmt.Sprintf(" %d: ", r.Win.Index)
			rest := truncate(name+mark, width-len([]rune(prefix))-2)
			return prefix + styleClaude.Render(icon) + " " + rest
		}
		return truncate(line, width)

	case rowPane:
		mark := " "
		if r.Pane.Active {
			mark = "*"
		}
		icon, name, isClaude := paneGlyph(*r.Pane)
		line := plain(fmt.Sprintf("  %d: %s %s%s", r.Pane.Index, icon, name, mark))
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		if isClaude {
			prefix := indent + fmt.Sprintf("  %d: ", r.Pane.Index)
			rest := truncate(name+mark, width-len([]rune(prefix))-2)
			return styleDim.Render(prefix) + styleClaude.Render(icon) + " " + styleDim.Render(rest)
		}
		return styleDim.Render(truncate(line, width))
	}
	return ""
}

func (m model) View() string {
	w, h := m.width, m.height
	if h < 4 {
		h = 4
	}
	lines := make([]string, h)
	lines[0] = styleTitle.Render(truncate(" bmux", w))

	// keep the cursor inside the viewport
	visible := h - 2 // title + footer
	offset := m.offset
	if m.cursor < offset {
		offset = m.cursor
	}
	if m.cursor >= offset+visible {
		offset = m.cursor - visible + 1
	}
	for i := 0; i < visible && offset+i < len(m.rows); i++ {
		lines[1+i] = m.renderRow(m.rows[offset+i], offset+i == m.cursor && m.mode == modeNormal)
	}

	footer := "⏎ jump  a new  d del  l/h fold  q quit"
	if m.footer != "" {
		footer = m.footer
	}
	lines[h-1] = styleFooter.Render(truncate(footer, w))

	out := strings.Join(lines, "\n")
	if box := m.modalView(); box != "" {
		out = overlayCenter(out, box, w, h)
	}
	return out
}

// modalView renders the centered pop-up for prompt/confirm/busy modes.
func (m model) modalView() string {
	boxW := m.width - 6
	if boxW > 46 {
		boxW = 46
	}
	if boxW < 20 {
		boxW = 20
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(boxW)

	switch m.mode {
	case modePrompt:
		return box.BorderForeground(lipgloss.Color("3")).Render(
			stylePrompt.Render("new worktree") + "\n" +
				m.input + "▎\n" +
				styleFooter.Render("⏎ create · esc cancel · empty = auto"))
	case modeConfirm:
		return box.BorderForeground(lipgloss.Color("1")).Render(
			m.confirmMsg + "\n" +
				styleFooter.Render("y confirm · any other key cancels"))
	case modeBusy:
		return box.BorderForeground(lipgloss.Color("3")).Render(m.busyMsg)
	}
	return ""
}

// overlayCenter splices box over the middle of bg (both ANSI-styled),
// neotree-popup style: the tree stays visible around the modal.
func overlayCenter(bg, box string, w, h int) string {
	bgLines := strings.Split(bg, "\n")
	boxLines := strings.Split(box, "\n")
	boxW := lipgloss.Width(box)
	top := (h - len(boxLines)) / 2
	if top < 0 {
		top = 0
	}
	left := (w - boxW) / 2
	if left < 0 {
		left = 0
	}
	for i, bl := range boxLines {
		r := top + i
		if r < 0 || r >= len(bgLines) {
			break
		}
		line := bgLines[r]
		if lw := lipgloss.Width(line); lw < w {
			line += strings.Repeat(" ", w-lw)
		}
		// Reset styling at both seams so the underlying row's colors don't
		// bleed into the box or vice versa.
		bgLines[r] = ansi.Truncate(line, left, "") + "\x1b[0m" + bl + "\x1b[0m" +
			ansi.TruncateLeft(line, left+boxW, "")
	}
	return strings.Join(bgLines, "\n")
}
