package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// macOS: /var/folders symlinks to /private/var; git reports resolved paths.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestGitBackendCreateRemove(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("BMUX_WORKTREE_DIR", t.TempDir())

	var b gitBackend
	path, err := b.Create(repo, "scratch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Fatalf("worktree not created at %s: %v", path, err)
	}

	wts := repoWorktrees(repo)
	if len(wts) != 2 {
		t.Fatalf("want 2 worktrees, got %+v", wts)
	}
	if wts[0].IsMain != true || wts[1].Branch != "scratch" {
		t.Fatalf("unexpected worktrees: %+v", wts)
	}

	if err := b.Remove(repo, path, "scratch"); err != nil {
		t.Fatal(err)
	}
	if wts := repoWorktrees(repo); len(wts) != 1 {
		t.Fatalf("worktree not removed: %+v", wts)
	}
}

func TestGitBackendAutoName(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("BMUX_WORKTREE_DIR", t.TempDir())

	var b gitBackend
	p1, err := b.Create(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := b.Create(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatalf("auto-names collided: %s", p1)
	}
}

func TestRepoWorktreesSkipsPrunable(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("BMUX_WORKTREE_DIR", t.TempDir())

	var b gitBackend
	path, err := b.Create(repo, "doomed")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// Deleted without `git worktree prune`: must not be listed.
	for _, wt := range repoWorktrees(repo) {
		if wt.Path == path {
			t.Fatalf("prunable worktree still listed: %s", path)
		}
	}
}

func TestRepoMainRootFromWorktree(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("BMUX_WORKTREE_DIR", t.TempDir())

	var b gitBackend
	path, err := b.Create(repo, "wt")
	if err != nil {
		t.Fatal(err)
	}
	if got := repoMainRoot(repoToplevel(path)); got != repo {
		t.Fatalf("main root from worktree = %q, want %q", got, repo)
	}
	if got := repoMainRoot(repoToplevel(repo)); got != repo {
		t.Fatalf("main root from main = %q, want %q", got, repo)
	}
}

func TestRegistryRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reg := loadRegistry()
	if len(reg.Repos) != 0 {
		t.Fatalf("fresh registry not empty: %+v", reg.Repos)
	}
	reg.upsert([]string{"/a", "/b", "/a", ""})
	reg = loadRegistry()
	if len(reg.Repos) != 2 {
		t.Fatalf("want [/a /b], got %+v", reg.Repos)
	}
	reg.remove("/a")
	reg = loadRegistry()
	if len(reg.Repos) != 1 || reg.Repos[0] != "/b" {
		t.Fatalf("want [/b], got %+v", reg.Repos)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	if got := sanitizeSessionName("a.b:c"); got != "a-b-c" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeSessionName(""); got != "session" {
		t.Fatalf("got %q", got)
	}
}
