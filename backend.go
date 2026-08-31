package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// worktreeBackend abstracts worktree creation/removal so bmux works with or
// without roachdev installed.
type worktreeBackend interface {
	// Create makes a worktree named name (empty = auto-generate) on a new
	// branch, for the repo rooted at mainRoot. Returns the worktree path.
	Create(mainRoot, name string) (string, error)
	// Remove deletes the worktree at path (named name) from mainRoot's
	// repo. force discards uncommitted changes.
	Remove(mainRoot, path, name string, force bool) error
	// DiscoverRoots returns extra main-repo roots this backend knows about.
	DiscoverRoots() []string
}

func selectBackend() worktreeBackend {
	if _, err := exec.LookPath("roachdev"); err == nil {
		return roachdevBackend{}
	}
	return gitBackend{}
}

// --- roachdev ---

type roachdevBackend struct{}

func (roachdevBackend) Create(mainRoot, name string) (string, error) {
	args := []string{"wt", "create"}
	if name != "" {
		args = append(args, name)
	}
	args = append(args, "--branch", "-C", mainRoot)
	out, err := exec.Command("roachdev", args...).Output()
	if err != nil {
		return "", commandError("roachdev wt create", err)
	}
	// stdout is the worktree path (designed for `cd $(roachdev wt create)`).
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		return "", fmt.Errorf("roachdev wt create: no path in output")
	}
	return lines[len(lines)-1], nil
}

func (roachdevBackend) Remove(mainRoot, path, name string, force bool) error {
	args := []string{"wt", "rm", name}
	if force {
		args = append(args, "-f")
	}
	if err := exec.Command("roachdev", args...).Run(); err != nil {
		return commandError("roachdev wt rm", err)
	}
	return nil
}

func (roachdevBackend) DiscoverRoots() []string {
	out, err := exec.Command("roachdev", "wt", "list").Output()
	if err != nil {
		return nil
	}
	var roots []string
	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if i == 0 { // header row: NAME REF PATH
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		wtPath := fields[len(fields)-1]
		top := repoToplevel(wtPath)
		if top == "" {
			continue
		}
		root := repoMainRoot(top)
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots
}

// --- plain git ---

type gitBackend struct{}

func worktreeBaseDir() string {
	if d := os.Getenv("BMUX_WORKTREE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".worktrees")
}

func (gitBackend) Create(mainRoot, name string) (string, error) {
	repoName := filepath.Base(mainRoot)
	if name == "" {
		for i := 1; ; i++ {
			cand := fmt.Sprintf("wt-%d", i)
			if _, err := os.Stat(filepath.Join(worktreeBaseDir(), repoName+"-"+cand)); os.IsNotExist(err) {
				name = cand
				break
			}
		}
	}
	dest := filepath.Join(worktreeBaseDir(), repoName+"-"+name)
	if err := os.MkdirAll(worktreeBaseDir(), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "-C", mainRoot, "worktree", "add", "-b", name, dest)
	if err := cmd.Run(); err != nil {
		return "", commandError("git worktree add", err)
	}
	return dest, nil
}

func (gitBackend) Remove(mainRoot, path, name string, force bool) error {
	args := []string{"-C", mainRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if err := exec.Command("git", args...).Run(); err != nil {
		return commandError("git worktree remove", err)
	}
	return nil
}

func (gitBackend) DiscoverRoots() []string { return nil }

func commandError(what string, err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		msg := strings.TrimSpace(string(ee.Stderr))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("%s: %s", what, msg)
	}
	return fmt.Errorf("%s: %v", what, err)
}
