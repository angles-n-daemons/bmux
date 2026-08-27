package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
		return "▾"
	}
	return "▸"
}

// renderRow builds one display line. Selected rows are rendered plain and
// reversed so the cursor is readable regardless of inner styling.
func (m model) renderRow(r row, selected bool) string {
	indent := strings.Repeat("  ", r.Depth)
	width := m.width

	plain := func(parts ...string) string { return indent + strings.Join(parts, "") }

	switch r.Kind {
	case rowRepo:
		line := plain(m.foldMarker(r), " ", r.Repo.Name)
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
		line := plain(m.foldMarker(r), " ", fmt.Sprintf("%d: %s%s", r.Win.Index, r.Win.Name, mark))
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		return truncate(line, width)

	case rowPane:
		mark := " "
		if r.Pane.Active {
			mark = "*"
		}
		line := plain(fmt.Sprintf("  %d: %s%s", r.Pane.Index, r.Pane.Command, mark))
		if selected {
			return styleCursor.Render(truncate(line, width))
		}
		return styleDim.Render(truncate(line, width))
	}
	return ""
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(truncate(" bmux", m.width)))
	b.WriteString("\n")

	// keep the cursor inside the viewport
	visible := m.height - 3 // title + footer + spare
	if visible < 1 {
		visible = 1
	}
	offset := m.offset
	if m.cursor < offset {
		offset = m.cursor
	}
	if m.cursor >= offset+visible {
		offset = m.cursor - visible + 1
	}

	for i := offset; i < len(m.rows) && i < offset+visible; i++ {
		b.WriteString(m.renderRow(m.rows[i], i == m.cursor && m.mode == modeNormal))
		b.WriteString("\n")
	}
	for i := len(m.rows) - offset; i < visible; i++ {
		b.WriteString("\n")
	}

	switch m.mode {
	case modePrompt:
		b.WriteString(stylePrompt.Render(truncate("new worktree (empty=auto): "+m.input+"▎", m.width)))
	case modeConfirm:
		b.WriteString(styleError.Render(truncate(m.confirmMsg+" [y/N]", m.width)))
	case modeBusy:
		b.WriteString(stylePrompt.Render(truncate(m.busyMsg, m.width)))
	default:
		if m.footer != "" {
			b.WriteString(styleFooter.Render(truncate(m.footer, m.width)))
		} else {
			b.WriteString(styleFooter.Render(truncate("⏎ jump  a new  d del  l/h fold  q quit", m.width)))
		}
	}
	return b.String()
}
