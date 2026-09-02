package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

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

const (
	iconClaude = "✳" // eight-spoked asterisk (Anthropic clay)
	iconCodex  = "✦" // four-pointed star (OpenAI green)
)

var (
	styleClaude = lipgloss.NewStyle().Foreground(lipgloss.Color("#d97757"))
	styleCodex  = lipgloss.NewStyle().Foreground(lipgloss.Color("#10a37f"))
)

// paneGlyph returns the icon (plain glyph), display name, the accent style for
// agent panes, and whether this pane is a coding agent (Claude or Codex).
func paneGlyph(p Pane) (icon, name string, accent lipgloss.Style, isAgent bool) {
	switch kind, n := agentOf(p); kind {
	case agentClaude:
		return iconClaude, n, styleClaude, true
	case agentCodex:
		return iconCodex, n, styleCodex, true
	}
	if ic, ok := commandIcons[p.Command]; ok {
		return ic, p.Command, lipgloss.Style{}, false
	}
	return defaultIcon, p.Command, lipgloss.Style{}, false
}

var (
	styleRepo    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleCurrent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	// Session titles: bold always; lightness signals online vs offline.
	styleNameLive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("253"))
	styleNameOff  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("243"))
	styleLive     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleCursor   = lipgloss.NewStyle().Reverse(true)
	styleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWaiting  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleStopped  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTitle    = lipgloss.NewStyle().Bold(true)
	// Title-bar status (costs): Anthropic clay, same as the ✳ marks —
	// it's Claude spend, so it wears Claude's color.
	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("#d97757"))
	styleFooter   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stylePrompt   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
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

// agentGlyph is a single pane's status indicator, badge-colored.
func agentGlyph(s agentStatus) string {
	switch s {
	case agentRunning:
		return styleRunning.Render("▶")
	case agentWaiting:
		return styleWaiting.Render("⏸")
	}
	return styleStopped.Render("⏹")
}

// paneBadge returns the trailing indicator for a Claude pane row.
func (m model) paneBadge(paneID string) string {
	if s, ok := m.snap.AgentByPane[paneID]; ok {
		return " " + agentGlyph(s)
	}
	return ""
}

// windowAgents aggregates agent statuses across a window's panes.
func (m model) windowAgents(session string, window int) []agentStatus {
	var out []agentStatus
	for _, p := range m.snap.Panes[session] {
		if p.Window == window {
			if s, ok := m.snap.AgentByPane[p.ID]; ok {
				out = append(out, s)
			}
		}
	}
	sortAgentStatuses(out)
	return out
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
	// Keep a two-cell right margin so truncated rows never touch the edge.
	width := m.width - 2

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
			if r.WT.WT.IsMain {
				// Git's term for the primary checkout; avoids repo → repo.
				name = "main"
			} else if r.Repo != nil && r.Repo.Root != "" {
				// Drop the redundant repo prefix (cockroach-primary → primary).
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
		// Aggregate badge only while collapsed; expanded rows hand the
		// indicators down to their children.
		badge := ""
		if !(r.expandable() && m.isExpanded(r)) {
			badge = agentBadge(agents)
		}

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
		var icon, name, badge string
		var accent lipgloss.Style
		var isAgent bool
		if r.SinglePane != nil {
			// A one-pane window IS its pane: render the pane, don't expand.
			icon, name, accent, isAgent = paneGlyph(*r.SinglePane)
			if isAgent {
				badge = m.paneBadge(r.SinglePane.ID)
			}
		} else {
			icon, name = defaultIcon, r.Win.Name
			if ap := m.activePaneOf(r.Win.Session, r.Win.Index); ap != nil {
				icon, _, accent, isAgent = paneGlyph(*ap)
			}
			if !m.isExpanded(r) {
				badge = agentBadge(m.windowAgents(r.Win.Session, r.Win.Index))
			}
		}
		line := plain(m.foldMarker(r), " ", icon, " ", name, mark)
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		room := width - lipgloss.Width(badge)
		if isAgent {
			prefix := indent + m.foldMarker(r) + " "
			rest := truncate(name+mark, room-len([]rune(prefix))-2)
			return prefix + accent.Render(icon) + " " + rest + badge
		}
		return truncate(line, room) + badge

	case rowPane:
		mark := " "
		if r.Pane.Active {
			mark = "*"
		}
		icon, name, accent, isAgent := paneGlyph(*r.Pane)
		badge := ""
		if isAgent {
			badge = m.paneBadge(r.Pane.ID)
		}
		// Two-cell gutter (the fold-marker slot) so panes sit one visual
		// step deeper than their window row.
		line := plain("  ", icon, " ", name, mark)
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		room := width - lipgloss.Width(badge)
		if isAgent {
			prefix := indent + "  "
			rest := truncate(name+mark, room-len([]rune(prefix))-2)
			return styleDim.Render(prefix) + accent.Render(icon) + " " + styleDim.Render(rest) + badge
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
	if s := m.snap.Status; s != "" {
		if pad := w - len([]rune(" bmux")) - len([]rune(s)) - 1; pad > 0 {
			lines[0] = styleTitle.Render(" bmux") + strings.Repeat(" ", pad) + styleStatus.Render(s) + " "
		}
	}

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

	lines[h-1] = m.hintLine(w)

	out := strings.Join(lines, "\n")
	if box := m.modalView(); box != "" {
		out = overlayCenter(out, box, w, h)
	}
	return out
}

// hintLine renders the footer: a transient notice when present, otherwise
// hints for what the cursor is on.
func (m model) hintLine(width int) string {
	if m.footer != "" {
		return styleFooter.Render(truncate(m.footer, width))
	}
	// Hints list only actions that work on the current row; fold hints
	// track the row's actual state.
	hints := "? help"
	if r := m.cur(); r != nil {
		fold := ""
		if r.expandable() {
			if m.isExpanded(*r) {
				fold = " · h fold"
			} else {
				fold = " · l unfold"
			}
		}
		switch {
		case r.Kind == rowRepo:
			hints = "a new worktree · ? help"
		case r.Kind == rowWorktree && r.WT.Session == nil:
			hints = "⏎ start session · d remove · ? help"
		case r.Kind == rowWorktree || r.Kind == rowSession:
			hints = "⏎ jump" + fold + " · d delete · ? help"
		default: // windows and panes
			hints = "⏎ jump" + fold + " · ? help"
		}
	}
	return styleFooter.Render(truncate(hints, width))
}

// helpView is the ? key reference, grouped and column-aligned.
func (m model) helpView(boxW int) string {
	key := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	group := lipgloss.NewStyle().Bold(true)
	dim := styleFooter

	entry := func(k, desc string) string {
		pad := 7 - lipgloss.Width(k)
		if pad < 1 {
			pad = 1
		}
		return "  " + key.Render(k) + strings.Repeat(" ", pad) + desc
	}
	lines := []string{
		group.Render("Navigate"),
		entry("j k", "move"+strings.Repeat(" ", 12)+dim.Render("g G  first / last")),
		entry("⏎", "open — jump, or start the session"),
		entry("l h", "unfold / fold (h twice: to parent)"),
		"",
		group.Render("Act"),
		entry("a", "new worktree, branch, and session"),
		entry("d", "delete what's under the cursor"),
		entry("r", "refresh & rediscover repos"),
		"",
		group.Render("Panel"),
		entry("q esc", "close"),
	}
	return modal.Lines(modal.Opts{
		Width: boxW, Title: "bmux",
		Footer: "any key closes",
		Accent: lipgloss.Color("6"),
	}, lines)
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
	case modeHelp:
		return m.helpView(boxW)
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
		// The modal rows become a clean band: splicing box edges into rows
		// containing nerd glyphs drifts by a column (PUA chars are counted
		// double-width by the ANSI truncation tables), so background text
		// on these rows is dropped instead of cut around.
		bgLines[r] = strings.Repeat(" ", left) + "\x1b[0m" + bl
	}
	return strings.Join(bgLines, "\n")
}
