package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/angles-n-daemons/bmux/modal"
)

// Nerd-font glyphs per foreground command; Claude panes are detected via
// their title marker instead and get ✳ in Anthropic clay.
// Written as \u escapes: literal PUA glyphs don't survive all tooling.
const (
	iconShell  = "" // nf-dev-terminal
	iconVim    = "" // nf-custom-vim
	iconNeovim = "" // nf-custom-neovim
	iconNode   = "" // nf-dev-nodejs_small
	iconPython = "" // nf-dev-python
	iconGo     = "" // nf-seti-go
	iconGit    = "" // nf-dev-git
	iconDocker = "" // nf-linux-docker
	iconServer = "" // nf-fa-server
	iconRust   = "" // nf-dev-rust
	iconWrench = "" // nf-fa-wrench
	iconGauge  = "" // nf-fa-dashboard
	iconBook   = "" // nf-fa-book

	defaultIcon = "" // nf-fa-terminal

	iconFolderClosed = "" // nf-fa-folder
	iconFolderOpen   = "" // nf-fa-folder_open
	iconChevronRight = "" // nf-cod-chevron_right
	iconChevronDown  = "" // nf-cod-chevron_down
)

var commandIcons = map[string]string{
	"zsh": iconShell, "bash": iconShell, "sh": iconShell, "fish": iconShell,
	"dash": iconShell, "ksh": iconShell,
	"nvim": iconNeovim, "vim": iconVim, "vi": iconVim,
	"node":   iconNode,
	"python": iconPython, "python3": iconPython, "ipython": iconPython,
	"go":  iconGo,
	"git": iconGit, "tig": iconGit, "lazygit": iconGit,
	"docker": iconDocker,
	"ssh":    iconServer, "mosh-client": iconServer,
	"cargo": iconRust, "rustc": iconRust,
	"make": iconWrench, "cmake": iconWrench,
	"htop": iconGauge, "top": iconGauge, "btop": iconGauge,
	"man": iconBook, "less": iconBook, "bat": iconBook,
}

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
	// Session titles: bold always; lightness signals online vs offline.
	styleNameLive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("253"))
	styleNameOff  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("243"))
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
		return iconChevronDown
	}
	return iconChevronRight
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
		icon := iconFolderClosed
		if m.isExpanded(r) {
			icon = iconFolderOpen
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
			base := filepath.Base(r.WT.WT.Path)
			name = base
			// Drop the redundant repo prefix (cockroach-primary → primary).
			if r.Repo != nil && r.Repo.Root != "" {
				if t := strings.TrimPrefix(name, r.Repo.Name+"-"); t != "" {
					name = t
				}
			}
			branch = r.WT.WT.Branch
			if r.WT.Session != nil {
				live = true
				agents = r.WT.Agents
				// Bracket the session name only when it differs from the
				// worktree's actual (untrimmed) name.
				if r.WT.Session.Name != base {
					sessName = r.WT.Session.Name
				}
			}
		} else {
			name = r.Sess.Session.Name
			live = true
			agents = r.Sess.Agents
		}

		head := plain(m.foldMarker(r), " ", name)
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

		// styled: bold titles, lighter when online, grayer when offline;
		// the current session gets its highlight color on top
		nameStyled := styleNameOff.Render(name)
		if live {
			nameStyled = styleNameLive.Render(name)
		}
		if r.sessionName() != "" && r.sessionName() == m.snap.CurrentSession {
			nameStyled = styleCurrent.Render(name)
		}
		out := indent + m.foldMarker(r) + " " + nameStyled
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
		var icon, name string
		var isClaude bool
		if r.SinglePane != nil {
			// A one-pane window IS its pane: render the pane, don't expand.
			icon, name, isClaude = paneGlyph(*r.SinglePane)
		} else {
			icon, name = defaultIcon, r.Win.Name
			if ap := m.activePaneOf(r.Win.Session, r.Win.Index); ap != nil {
				icon, _, isClaude = paneGlyph(*ap)
			}
		}
		line := plain(m.foldMarker(r), " ", icon, " ", name, mark)
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		if isClaude {
			prefix := indent + m.foldMarker(r) + " "
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
		// Two-cell gutter (the fold-marker slot) so panes sit one visual
		// step deeper than their window row.
		line := plain("  ", icon, " ", name, mark)
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		if isClaude {
			prefix := indent + "  "
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
	if boxW > 48 {
		boxW = 48
	}
	if boxW < 22 {
		boxW = 22
	}
	switch m.mode {
	case modePrompt:
		title := "New worktree"
		if m.promptRepo != "" {
			title += " · " + filepath.Base(m.promptRepo)
		}
		return modal.Input(modal.Opts{
			Width: boxW, Title: title,
			Footer: "⏎ create · esc cancel · empty = auto",
			Accent: lipgloss.Color("3"),
		}, m.input)
	case modeConfirm:
		return modal.Confirm(modal.Opts{
			Width: boxW, Title: m.confirmTitle,
			Footer: "y confirm · n cancel",
			Accent: lipgloss.Color("1"),
		}, m.confirmMsg)
	case modeBusy:
		return modal.Notice(modal.Opts{
			Width: boxW, Title: "Working",
			Accent: lipgloss.Color("3"),
		}, m.busyMsg)
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
