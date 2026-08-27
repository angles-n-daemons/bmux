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

// panelWidth is a quarter of the window, at least 40 columns, at most 40%.
// Override with `set -g @bmux_width 80` (columns) or `set -g @bmux_width 30%`.
func panelWidth(target string) int {
	args := []string{"display-message", "-p"}
	if target != "" {
		args = append(args, "-t", target)
	}
	args = append(args, "#{window_width}")
	out, _ := tmuxOut(args...)
	winW, _ := strconv.Atoi(strings.TrimSpace(out))
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
	w := winW / 4
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

// findPanelPane returns the traveling panel's pane id, wherever it lives.
func findPanelPane() string {
	out, err := tmuxOut("list-panes", "-a", "-F", "#{pane_id}"+sep+"#{@bmux_panel}")
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

// cmdToggle flips the global panel: closes it wherever it is, or opens it
// in the current window and marks the server-wide state open.
func cmdToggle() error {
	if !insideTmux() {
		return fmt.Errorf("toggle only works inside tmux")
	}
	if id := findPanelPane(); id != "" {
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
	if id := findPanelPane(); id != "" {
		return tmuxRun("join-pane", "-hbdf", "-l", strconv.Itoa(panelWidth(win)), "-s", id, "-t", win)
	}
	return spawnPanel(win, false)
}

// activeWindowID resolves the window the (first) attached client shows.
func activeWindowID() string {
	out, err := tmuxOut("list-clients", "-F", "#{client_session}")
	if err != nil {
		return ""
	}
	sess := strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0]
	if sess == "" {
		return ""
	}
	out, err = tmuxOut("list-windows", "-t", "="+sess, "-F", "#{window_id}"+sep+"#{window_active}")
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
