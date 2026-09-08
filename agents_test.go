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

func TestClaudeHasBackgroundWork(t *testing.T) {
	// Footer region shapes captured from live Claude panes.
	withShells := []string{
		"※ recap: pushing the PR",
		"❯ ",
		"  ⏸ manual mode on · 2 shells · ← for agents      100% context used",
	}
	oneShell := []string{"  ⏸ manual mode on · 1 shell · ← for agents"}
	withSubAgent := []string{
		"  ⏺ main",
		"  ◯ general-purpose (+1)  Writing report.go   39m 36s · ↓ 473.2k tokens",
	}
	idle := []string{
		"※ recap: all done, CI green",
		"  ⏸ manual mode on · ? for shortcuts · ← for agents",
	}
	// "N shells" mentioned only in stale scrollback (outside the footer tail)
	// must not count as live background work.
	staleScrollback := append([]string{
		"⏺ I launched 3 shells earlier to run the sweep.",
	}, make([]string, 12)...)

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"two shells", withShells, true},
		{"one shell", oneShell, true},
		{"sub-agent", withSubAgent, true},
		{"idle", idle, false},
		{"stale scrollback", staleScrollback, false},
	}
	for _, c := range cases {
		if got := claudeHasBackgroundWork(c.lines); got != c.want {
			t.Errorf("claudeHasBackgroundWork(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodexHasBackgroundWork(t *testing.T) {
	running := []string{
		"• It worked. I created and pushed the branch.",
		"  1 background terminal running · /ps to view · /stop to close",
		"",
		"› Ask Codex to do anything",
		"  gpt-5.6-sol default fast · ~/go/src/roachdev",
	}
	plural := []string{"  2 background terminals running · /ps to view · /stop to close"}
	idle := []string{
		"• Done.",
		"› Ask Codex to do anything",
		"  gpt-5.6-sol default fast · ~/go/src/roachdev",
	}
	// A stale mention outside the footer tail must not count.
	stale := append([]string{
		"• Earlier I had 1 background terminal running for the sweep.",
	}, make([]string, 12)...)

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"one terminal", running, true},
		{"two terminals", plural, true},
		{"idle", idle, false},
		{"stale scrollback", stale, false},
	}
	for _, c := range cases {
		if got := codexHasBackgroundWork(c.lines); got != c.want {
			t.Errorf("codexHasBackgroundWork(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSortAgentStatusesPriority(t *testing.T) {
	// Running > waiting > background > stopped.
	s := []agentStatus{agentStopped, agentBackground, agentRunning, agentWaiting}
	sortAgentStatuses(s)
	want := []agentStatus{agentRunning, agentWaiting, agentBackground, agentStopped}
	for i := range want {
		if s[i] != want[i] {
			t.Fatalf("sorted = %v, want %v", s, want)
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
