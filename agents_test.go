package main

import "testing"

func TestClassifyCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want agentKind
	}{
		{"roachdev claude --safe --", agentClaude},
		{"roachdev codex", agentCodex},
		{"claude --settings /tmp/x.json --model opus", agentClaude},
		{"codex --config shell_environment_policy.exclude=[]", agentCodex},
		{"/Users/b/.codex/packages/standalone/releases/0.1/codex-abc", agentCodex},
		{"/Users/b/go/bin/roachdev codex", agentCodex},
		{"-zsh", agentNone},
		{"caffeinate -i -t 300", agentNone},
		{"vim main.go", agentNone},
		{"roachdev wt list", agentNone},
		{"", agentNone},
	}
	for _, c := range cases {
		if got := classifyCommand(c.cmd); got != c.want {
			t.Errorf("classifyCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestAgentKindsByPane(t *testing.T) {
	// Two panes, each a zsh hosting a roachdev-wrapped agent (grandchild is the
	// real binary) — the shape seen live. A third pane is a plain shell.
	table := map[int]procInfo{
		100: {ppid: 1, kind: agentNone},     // pane A: zsh
		101: {ppid: 100, kind: agentClaude}, // roachdev claude
		102: {ppid: 101, kind: agentClaude}, // claude binary
		200: {ppid: 1, kind: agentNone},     // pane B: zsh
		201: {ppid: 200, kind: agentCodex},  // roachdev codex
		202: {ppid: 201, kind: agentCodex},  // codex binary
		300: {ppid: 1, kind: agentNone},     // pane C: plain zsh
	}
	panes := []Pane{
		{ID: "%a", PID: 100},
		{ID: "%b", PID: 200},
		{ID: "%c", PID: 300},
	}
	kinds := agentKindsByPane(panes, table)
	if kinds["%a"] != agentClaude {
		t.Errorf("pane A = %v, want claude", kinds["%a"])
	}
	if kinds["%b"] != agentCodex {
		t.Errorf("pane B = %v, want codex", kinds["%b"])
	}
	if _, ok := kinds["%c"]; ok {
		t.Errorf("plain shell pane should not be an agent")
	}
}

func TestCodexStatus(t *testing.T) {
	cases := []struct {
		title string
		want  agentStatus
	}{
		{"cockroach-oncall", agentStopped},
		{"⠧ Analyze disk usage on macOS", agentRunning},
		{"⠇ bmux", agentRunning},
		{"[ . ] Action Required | cockroach-oncall", agentWaiting},
	}
	for _, c := range cases {
		if got := codexStatus(Pane{Title: c.title}); got != c.want {
			t.Errorf("codexStatus(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestAgentDisplayName(t *testing.T) {
	cases := []struct {
		kind  agentKind
		title string
		want  string
	}{
		{agentClaude, "✳ Fix the bug", "Fix the bug"},
		{agentClaude, "⠹ Fix the bug", "Fix the bug"},
		{agentClaude, "plain", "claude"},
		{agentCodex, "⠐ Analyze disk usage on macOS", "Analyze disk usage on macOS"},
		{agentCodex, "cockroach-oncall", "codex"},
		{agentCodex, "[ . ] Action Required | cockroach-oncall", "codex"},
	}
	for _, c := range cases {
		if got := agentDisplayName(c.kind, c.title); got != c.want {
			t.Errorf("agentDisplayName(%v, %q) = %q, want %q", c.kind, c.title, got, c.want)
		}
	}
}
