package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	p := tea.NewProgram(newModel(selectBackend()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fail(err)
	}
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
	paneID, err := tmuxOut("split-window", "-hbf", "-l", "34", "-P", "-F", "#{pane_id}", self)
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
