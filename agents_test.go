package main

import "testing"

func TestAgentOf(t *testing.T) {
	cases := []struct {
		name     string
		pane     Pane
		wantKind agentKind
		wantName string
	}{
		{"claude idle", Pane{Title: "✳ Fix the bug", Command: "claude"}, agentClaude, "Fix the bug"},
		{"claude running", Pane{Title: "⠹ Fix the bug", Command: "roachdev"}, agentClaude, "Fix the bug"},
		{"claude reverted to shell", Pane{Title: "✳ Fix the bug", Command: "zsh"}, agentNone, ""},
		{"codex idle", Pane{Title: "bmux", Command: "codex"}, agentCodex, "codex"},
		{"codex running", Pane{Title: "⠧ bmux", Command: "codex"}, agentCodex, "codex"},
		{"codex waiting", Pane{Title: "[ . ] Action Required | bmux", Command: "codex"}, agentCodex, "codex"},
		{"plain shell", Pane{Title: "Brians-MacBook-Pro.local", Command: "zsh"}, agentNone, ""},
		{"vim", Pane{Title: "somefile.go", Command: "vim"}, agentNone, ""},
	}
	for _, c := range cases {
		gotKind, gotName := agentOf(c.pane)
		if gotKind != c.wantKind || gotName != c.wantName {
			t.Errorf("%s: agentOf = (%v, %q), want (%v, %q)", c.name, gotKind, gotName, c.wantKind, c.wantName)
		}
	}
}

func TestCodexStatus(t *testing.T) {
	cases := []struct {
		title string
		want  agentStatus
	}{
		{"bmux", agentStopped},
		{"⠧ bmux", agentRunning},
		{"⠇ bmux", agentRunning},
		{"[ . ] Action Required | bmux", agentWaiting},
	}
	for _, c := range cases {
		if got := codexStatus(Pane{Title: c.title, Command: "codex"}); got != c.want {
			t.Errorf("codexStatus(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

// A running Codex title carries a braille spinner too; it must not be
// misread as Claude.
func TestCodexNotMistakenForClaude(t *testing.T) {
	p := Pane{Title: "⠧ bmux", Command: "codex"}
	if kind, _ := agentOf(p); kind != agentCodex {
		t.Fatalf("spinning codex classified as %v, want agentCodex", kind)
	}
}

func TestDetectAgentsMixed(t *testing.T) {
	panes := []Pane{
		{ID: "%1", Session: "s1", Title: "⠧ bmux", Command: "codex"},                       // codex running
		{ID: "%2", Session: "s1", Title: "[ . ] Action Required | env", Command: "codex"},   // codex waiting
		{ID: "%3", Session: "s2", Title: "✳ Fix the bug", Command: "claude"},                // claude idle -> stopped
		{ID: "%4", Session: "s2", Title: "Brians-MacBook-Pro.local", Command: "zsh"},        // not an agent
	}
	_, byPane := detectAgents(panes)
	if byPane["%1"] != agentRunning {
		t.Errorf("codex running pane = %v", byPane["%1"])
	}
	if byPane["%2"] != agentWaiting {
		t.Errorf("codex waiting pane = %v", byPane["%2"])
	}
	if _, ok := byPane["%4"]; ok {
		t.Errorf("non-agent pane should not be tracked")
	}
}
