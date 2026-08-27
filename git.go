package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Path   string
	Branch string // short branch name, or short SHA when detached
	IsMain bool
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// repoToplevel returns the worktree root containing path, or "" if not a repo.
func repoToplevel(path string) string {
	out, err := gitOut(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return out
}

// repoMainRoot resolves any checkout (main or linked worktree) to the main
// repository root via the shared git common dir.
func repoMainRoot(toplevel string) string {
	common, err := gitOut(toplevel, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || common == "" {
		return toplevel
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common // bare-ish edge; better than nothing
}

// repoWorktrees lists all checkouts of the repo rooted at mainRoot. The first
// entry reported by git is the main checkout.
func repoWorktrees(mainRoot string) []Worktree {
	out, err := gitOut(mainRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var wts []Worktree
	var cur *Worktree
	first := true
	flush := func() {
		// Skip prunable entries: worktrees whose directory was deleted
		// without `git worktree prune` are still listed by git.
		if cur != nil {
			if _, err := os.Stat(cur.Path); err == nil {
				wts = append(wts, *cur)
			}
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree "), IsMain: first}
			first = false
		case cur == nil:
			continue
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case strings.HasPrefix(line, "HEAD ") && cur.Branch == "":
			sha := strings.TrimPrefix(line, "HEAD ")
			if len(sha) > 9 {
				sha = sha[:9]
			}
			cur.Branch = sha
		}
	}
	flush()
	return wts
}
