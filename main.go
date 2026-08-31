package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runTUI()
		return
	}
	switch args[0] {
	case "toggle":
		fail(cmdToggle())
	case "ensure":
		fail(cmdEnsure())
	case "go":
		if len(args) < 2 || (args[1] != "prev" && args[1] != "next") {
			fail(fmt.Errorf("usage: bmux go <prev|next>"))
		}
		fail(cmdGo(args[1]))
	case "up":
		fail(cmdUp())
	default:
		fmt.Fprintf(os.Stderr, "usage: bmux [toggle|ensure|up]\n\n"+
			"  (none)  run the navigator TUI (inside tmux)\n"+
			"  toggle  open/close the navigator panel (global, follows you)\n"+
			"  ensure  hook target: move the panel into the current window\n"+
			"  up      start sessions for all worktrees and attach to the tree\n")
		os.Exit(2)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "bmux: "+err.Error())
		os.Exit(1)
	}
}

func runTUI() {
	if !insideTmux() {
		fail(fmt.Errorf("not inside tmux; use `bmux up` to boot a server"))
	}
	stylePane()
	defer unstylePane()
	p := tea.NewProgram(newModel(selectBackend()), tea.WithAltScreen())
	_, err := p.Run()
	// Quitting the traveling panel with q closes it globally; otherwise the
	// window-change hooks would resurrect it on the next switch.
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if out, _ := tmuxOut("show-options", "-pqv", "-t", pane, "@bmux_panel"); strings.TrimSpace(out) == "1" {
			_ = tmuxRun("set-option", "-g", "@bmux_open", "0")
		}
	}
	fail(err)
}

// The panel is chrome, and chrome recedes: it uses the surrounding scheme's
// exact hues (so it never clashes) but desaturated and darkened, like an
// editor sidebar — the muted shadow of the theme rather than another
// content pane. It makes the same blur→focus hue journey as regular panes.
// Override with tmux user options:
//
//	set -g @bmux_window_style 'bg=#261724,fg=#9cc9ad'
//	set -g @bmux_window_active_style 'bg=#152627,fg=#d4c39b'
const (
	defaultPanelStyle       = "bg=#261724,fg=#9cc9ad" // blurred: muted plum / sage
	defaultPanelActiveStyle = "bg=#152627,fg=#d4c39b" // focused: muted teal / cream
)

func stylePane() {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return
	}
	style := userOption("@bmux_window_style", defaultPanelStyle)
	active := userOption("@bmux_window_active_style", defaultPanelActiveStyle)
	_ = tmuxRun("set-option", "-p", "-t", pane, "window-style", style)
	_ = tmuxRun("set-option", "-p", "-t", pane, "window-active-style", active)
}

// unstylePane restores inherited styles, for `bmux` runs in a pane that
// outlives the TUI (toggle/home panes die with it anyway).
func unstylePane() {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return
	}
	_ = tmuxRun("set-option", "-p", "-t", pane, "-u", "window-style")
	_ = tmuxRun("set-option", "-p", "-t", pane, "-u", "window-active-style")
}

// panelWidth is 22% of the window, at least 40 columns, at most 40%.
// Override with `set -g @bmux_width 80` (columns) or `set -g @bmux_width 30%`.
func panelWidth(target string) int {
	// Prefer the client width: background windows keep a stale default size
	// (80x24) until first displayed, which would skew the math.
	winW := 0
	if out, err := tmuxOut("list-clients", "-F", "#{client_width}"); err == nil {
		winW, _ = strconv.Atoi(strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0])
	}
	if winW <= 0 {
		args := []string{"display-message", "-p"}
		if target != "" {
			args = append(args, "-t", target)
		}
		args = append(args, "#{window_width}")
		out, _ := tmuxOut(args...)
		winW, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	if winW <= 0 {
		winW = 160
	}
	if opt := userOption("@bmux_width", ""); opt != "" {
		if pct, ok := strings.CutSuffix(opt, "%"); ok {
			if n, err := strconv.Atoi(pct); err == nil && n > 0 && n < 100 {
				return winW * n / 100
			}
		} else if n, err := strconv.Atoi(opt); err == nil && n > 0 {
			return n
		}
	}
	w := winW * 22 / 100
	if w < 40 {
		w = 40
	}
	if cap := winW * 40 / 100; w > cap {
		w = cap
	}
	return w
}

func userOption(name, fallback string) string {
	out, err := tmuxOut("show-options", "-gqv", name)
	if err != nil || strings.TrimSpace(out) == "" {
		return fallback
	}
	return strings.TrimSpace(out)
}

// The panel is global to the server: one pane that travels. @bmux_open
// holds the open/closed state; hooks on window/session changes call
// `bmux ensure`, which join-panes the live panel pane (TUI process and all)
// into whatever window the client now shows.

// findPanelPane returns the traveling panel's pane id and the window
// holding it, wherever it lives.
func findPanelPane() (paneID, windowID string) {
	out, err := tmuxOut("list-panes", "-a", "-F", "#{pane_id}"+sep+"#{window_id}"+sep+"#{@bmux_panel}")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 3)
		if len(parts) == 3 && parts[2] == "1" {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

// cmdToggle flips the global panel: closes it wherever it is, or opens it
// in the current window and marks the server-wide state open.
func cmdToggle() error {
	if !insideTmux() {
		return fmt.Errorf("toggle only works inside tmux")
	}
	if id, _ := findPanelPane(); id != "" {
		_ = tmuxRun("set-option", "-g", "@bmux_open", "0")
		return tmuxRun("kill-pane", "-t", id)
	}
	if err := spawnPanel("", true); err != nil {
		return err
	}
	return tmuxRun("set-option", "-g", "@bmux_open", "1")
}

// spawnPanel docks a fresh panel on the left of target (or the current
// window when target is empty). focus moves to the panel only when asked.
func spawnPanel(target string, focus bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"split-window", "-hbf", "-l", strconv.Itoa(panelWidth(target))}
	if !focus {
		args = append(args, "-d")
	}
	if target != "" {
		args = append(args, "-t", target)
	}
	args = append(args, "-P", "-F", "#{pane_id}", self)
	paneID, err := tmuxOut(args...)
	if err != nil {
		return err
	}
	return tmuxRun("set-option", "-p", "-t", strings.TrimSpace(paneID), "@bmux_panel", "1")
}

// cmdEnsure is the hook target: when the global panel is open, make sure it
// is docked in the window the client is looking at — moving the live pane
// (join-pane), or respawning it if the process died.
func cmdEnsure() error {
	if userOption("@bmux_open", "0") != "1" {
		return nil
	}
	win := activeWindowID()
	if win == "" {
		return nil
	}
	// Skip windows already showing bmux (the panel itself, or the home tree).
	out, err := tmuxOut("list-panes", "-t", win, "-F",
		"#{pane_current_command}"+sep+"#{@bmux_panel}")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 && (parts[1] == "1" || parts[0] == "bmux") {
			return nil
		}
	}
	if id, cur := findPanelPane(); id != "" {
		if cur == win {
			return nil
		}
		return tmuxRun("join-pane", "-hbdf", "-l", strconv.Itoa(panelWidth(win)), "-s", id, "-t", win)
	}
	return spawnPanel(win, false)
}

// cmdGo switches to the previous/next window, moving the open panel into
// the destination FIRST and issuing move+switch as one tmux sequence — the
// reflow happens while the destination window is still invisible, so the
// switch renders in a single clean frame (no halfway state).
func cmdGo(dir string) error {
	sess := clientSession()
	if sess == "" {
		return nil
	}
	out, err := tmuxOut("list-windows", "-t", "="+sess, "-F", "#{window_id}"+sep+"#{window_active}")
	if err != nil {
		return err
	}
	var wins []string
	cur := -1
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == "1" {
			cur = len(wins)
		}
		wins = append(wins, parts[0])
	}
	if len(wins) < 2 || cur < 0 {
		return nil
	}
	target := wins[(cur+1)%len(wins)]
	if dir == "prev" {
		target = wins[(cur-1+len(wins))%len(wins)]
	}
	var join []string
	panelID := ""
	w := 0
	if userOption("@bmux_open", "0") == "1" {
		if id, win := findPanelPane(); id != "" && win != target {
			panelID, w = id, panelWidth(target)
			join = []string{"join-pane", "-hbdf", "-l", strconv.Itoa(w), "-s", id, "-t", target}
		}
	}
	if err := tmuxSeq([]string{"select-window", "-t", target}, join); err != nil {
		return err
	}
	return correctPanelWidth(panelID, w)
}

func clientSession() string {
	out, err := tmuxOut("list-clients", "-F", "#{client_session}")
	if err != nil {
		return ""
	}
	return strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0]
}

// activeWindowID resolves the window the (first) attached client shows.
func activeWindowID() string {
	sess := clientSession()
	if sess == "" {
		return ""
	}
	return activeWindowOf(sess)
}

// activeWindowOf returns the window id a session currently shows.
func activeWindowOf(sess string) string {
	out, err := tmuxOut("list-windows", "-t", "="+sess, "-F", "#{window_id}"+sep+"#{window_active}")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 && parts[1] == "1" {
			return parts[0]
		}
	}
	return ""
}

// jumpTo navigates to a session (and optionally window/pane). When run from
// the traveling panel, the panel pane joins the destination window in the
// same tmux command sequence as the switch, so the destination is fully
// laid out before it becomes visible — one clean frame.
func jumpTo(session string, window, pane int) error {
	destWin := ""
	if window >= 0 {
		out, err := tmuxOut("display-message", "-p", "-t", fmt.Sprintf("=%s:%d", session, window), "#{window_id}")
		if err != nil {
			return err
		}
		destWin = strings.TrimSpace(out)
	} else {
		destWin = activeWindowOf(session)
	}

	var join []string
	panelID := ""
	w := 0
	if self := os.Getenv("TMUX_PANE"); self != "" && destWin != "" {
		if out, _ := tmuxOut("show-options", "-pqv", "-t", self, "@bmux_panel"); strings.TrimSpace(out) == "1" {
			ownWin, _ := tmuxOut("display-message", "-p", "-t", self, "#{window_id}")
			if strings.TrimSpace(ownWin) != destWin && !windowShowsBmux(destWin) {
				panelID, w = self, panelWidth(destWin)
				join = []string{"join-pane", "-hbdf", "-l", strconv.Itoa(w), "-s", self, "-t", destWin}
			}
		}
	}

	var sel, selPane []string
	if window >= 0 {
		sel = []string{"select-window", "-t", destWin}
		if pane >= 0 {
			selPane = []string{"select-pane", "-t", fmt.Sprintf("=%s:%d.%d", session, window, pane)}
		}
	}
	if err := tmuxSeq(sel, selPane, []string{"switch-client", "-t", "=" + session}, join); err != nil {
		return err
	}
	return correctPanelWidth(panelID, w)
}

// windowShowsBmux reports whether a window already contains a bmux pane
// (e.g. the home full-screen tree) — the panel never docks next to one.
func windowShowsBmux(win string) bool {
	out, err := tmuxOut("list-panes", "-t", win, "-F", "#{pane_current_command}")
	if err != nil {
		return false
	}
	for _, cmd := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if cmd == "bmux" {
			return true
		}
	}
	return false
}

// correctPanelWidth snaps the panel to its intended width after a move.
// Never-displayed windows keep a stale default size (80x24) until tmux
// recalculates at the end of the command batch, so a join into one gets
// proportionally rescaled; resize-pane fixes that without resize-window's
// sticky manual-sizing side effect. No-op when the width already matches.
func correctPanelWidth(panelID string, w int) error {
	if panelID == "" || w <= 0 {
		return nil
	}
	return tmuxRun("resize-pane", "-t", panelID, "-x", strconv.Itoa(w))
}

// cmdUp boots the full environment: a detached session for every worktree of
// every known repo, plus a "home" session running the tree full-screen. Then
// it attaches (or switches) to home. Idempotent.
func cmdUp() error {
	backend := selectBackend()

	sessions, _ := listSessions()
	claimed := map[string]bool{} // worktree paths that already have a session
	roots := map[string]bool{}
	var sessionRoots []string
	for _, s := range sessions {
		if top := repoToplevel(s.Path); top != "" {
			claimed[top] = true
			root := repoMainRoot(top)
			roots[root] = true
			sessionRoots = append(sessionRoots, root)
		}
	}
	reg := loadRegistry()
	reg.upsert(sessionRoots)
	for _, r := range backend.DiscoverRoots() {
		roots[r] = true
	}
	for _, r := range reg.Repos {
		roots[r] = true
	}

	for root := range roots {
		if repoToplevel(root) == "" {
			continue
		}
		for _, wt := range repoWorktrees(root) {
			if claimed[wt.Path] {
				continue
			}
			name := uniqueSessionName(filepath.Base(wt.Path))
			if err := newSession(name, wt.Path); err != nil {
				return err
			}
			claimed[wt.Path] = true
		}
	}

	if !hasSession("home") {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		home, _ := os.UserHomeDir()
		if err := newSession("home", home, self); err != nil {
			return err
		}
	}

	if insideTmux() {
		return switchClient("home")
	}
	// Replace ourselves with the attached client.
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	argv := []string{"tmux"}
	if sock := os.Getenv("BMUX_SOCKET"); sock != "" {
		argv = append(argv, "-L", sock)
	}
	argv = append(argv, "attach-session", "-t", "=home")
	return syscall.Exec(tmuxPath, argv, os.Environ())
}
