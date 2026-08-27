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
	case "up":
		fail(cmdUp())
	default:
		fmt.Fprintf(os.Stderr, "usage: bmux [toggle|up]\n\n"+
			"  (none)  run the navigator TUI (inside tmux)\n"+
			"  toggle  open/close the navigator as a left side panel\n"+
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
	if _, err := p.Run(); err != nil {
		fail(err)
	}
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
func panelWidth() int {
	out, _ := tmuxOut("display-message", "-p", "#{window_width}")
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

// cmdToggle opens the navigator as a full-height pane docked on the left of
// the current window, or closes it if one is already there.
func cmdToggle() error {
	if !insideTmux() {
		return fmt.Errorf("toggle only works inside tmux")
	}
	out, err := tmuxOut("list-panes", "-F", "#{pane_id}"+sep+"#{@bmux_panel}")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 && parts[1] == "1" {
			return tmuxRun("kill-pane", "-t", parts[0])
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	paneID, err := tmuxOut("split-window", "-hbf", "-l", strconv.Itoa(panelWidth()), "-P", "-F", "#{pane_id}", self)
	if err != nil {
		return err
	}
	return tmuxRun("set-option", "-p", "-t", strings.TrimSpace(paneID), "@bmux_panel", "1")
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
