package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRoachdevForceRemovesBrokenWorktree exercises the exact code path a user
// hits in the panel: force-deleting a roachdev worktree whose gitdir link has
// been severed (prunable, directory survives). Requires roachdev on PATH.
func TestRoachdevForceRemovesBrokenWorktree(t *testing.T) {
	if _, err := exec.LookPath("roachdev"); err != nil {
		t.Skip("roachdev not on PATH")
	}
	repo := initTestRepo(t)
	repoName := filepath.Base(repo)
	name := repoName + "-brokenwt-test"
	wtPath := filepath.Join(os.Getenv("HOME"), ".roachdev", "worktrees", name)

	// Create via roachdev exactly as bmux does.
	out, err := exec.Command("roachdev", "wt", "create", "brokenwt-test", "--branch", "-C", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("roachdev wt create: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("git", "-C", repo, "worktree", "prune").Run()
		os.RemoveAll(wtPath)
	})
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not created at %s: %v", wtPath, err)
	}

	// Sever the gitdir link: the real-world half-deleted worktree state.
	if err := os.Remove(filepath.Join(wtPath, ".git")); err != nil {
		t.Fatal(err)
	}

	var b roachdevBackend
	if err := b.Remove(repo, wtPath, name, true); err != nil {
		t.Fatalf("force removal of broken roachdev worktree failed: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree directory should be gone after force removal, still at %s", wtPath)
	}
}

// TestModelDeleteFlowBrokenWorktree drives the exact UI flow a user hits:
// d on a broken worktree -> confirm -> forceOffer -> confirm -> removed.
func TestModelDeleteFlowBrokenWorktree(t *testing.T) {
	if _, err := exec.LookPath("roachdev"); err != nil {
		t.Skip("roachdev not on PATH")
	}
	repo := initTestRepo(t)
	repoName := filepath.Base(repo)
	name := repoName + "-brokenflow-test"
	wtPath := filepath.Join(os.Getenv("HOME"), ".roachdev", "worktrees", name)

	out, err := exec.Command("roachdev", "wt", "create", "brokenflow-test", "--branch", "-C", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("roachdev wt create: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("git", "-C", repo, "worktree", "prune").Run()
		os.RemoveAll(wtPath)
	})
	if err := os.Remove(filepath.Join(wtPath, ".git")); err != nil {
		t.Fatal(err)
	}

	g := repoGroup{Root: repo, Name: repoName}
	wt := wtRow{WT: Worktree{Path: wtPath, Branch: "brokenflow-test"}}
	g.Worktrees = []wtRow{wt}
	m := model{
		backend: roachdevBackend{},
		snap:    snapshot{Repos: []repoGroup{g}},
		rows: []row{{
			Kind: rowWorktree,
			Key:  "wt:" + wtPath,
			Repo: &g,
			WT:   &g.Worktrees[0],
		}},
		expanded: map[string]bool{},
	}

	// d -> a single confirm (delete is always one confirm, then force-remove)
	nm, _ := m.startDelete()
	m = nm.(model)
	if m.mode != modeConfirm {
		t.Fatalf("after d, want modeConfirm, got %v", m.mode)
	}
	// y on the confirm -> should remove in one step, no second prompt
	msg := m.confirmFn()
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("broken worktree should delete in one confirm, got %T: %+v", msg, msg)
	}
	if done.err != nil {
		t.Fatalf("delete reported error: %v", done.err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after delete flow: %s", wtPath)
	}
}
