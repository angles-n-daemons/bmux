package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Coding-agent detection. Two agents are recognised — Claude Code and Codex —
// and they are told apart by walking each pane's process tree, not by the pane
// title or foreground command. In this environment both agents are launched
// through the `roachdev` wrapper (`roachdev claude` / `roachdev codex`), so a
// pane's foreground command is always `roachdev`, and a *running* Codex title
// (spinner + task) is shaped exactly like a running Claude title — neither is a
// reliable discriminator. The process subtree is: whichever of `codex` /
// `claude` runs underneath the pane wins.
//
// Status, once the kind is known, does come from the title/content:
//   - Claude: a braille spinner means working; an idle pane whose recent lines
//     show a permission prompt is "waiting"; otherwise stopped.
//   - Codex: "Action Required" in the title means waiting, a leading braille
//     spinner means working, otherwise stopped.

type agentStatus int

const (
	agentStopped agentStatus = iota
	agentWaiting
	agentRunning
)

type agentKind int

const (
	agentNone agentKind = iota
	agentClaude
	agentCodex
)

// procInfo is one row of the process table: a process's parent and which agent
// (if any) its command line represents.
type procInfo struct {
	ppid int
	kind agentKind
}

// classifyCommand maps a process's command line to an agent kind. It matches
// the agent invoked directly (`codex …` / `claude …`), through the roachdev
// wrapper (`roachdev codex` / `roachdev claude`), or the standalone Codex
// binary that ships hashed under ~/.codex.
func classifyCommand(cmd string) agentKind {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return agentNone
	}
	switch filepath.Base(fields[0]) {
	case "codex":
		return agentCodex
	case "claude":
		return agentClaude
	case "roachdev":
		if len(fields) >= 2 {
			switch fields[1] {
			case "codex":
				return agentCodex
			case "claude":
				return agentClaude
			}
		}
	}
	if strings.Contains(cmd, "/.codex/") {
		return agentCodex
	}
	return agentNone
}

// processTable reads the whole process table once (pid, ppid, command),
// classifying each command line. One `ps` fork per refresh.
func processTable() map[int]procInfo {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil
	}
	table := map[int]procInfo{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[pid] = procInfo{ppid: ppid, kind: classifyCommand(strings.Join(fields[2:], " "))}
	}
	return table
}

// subtreeAgent returns the agent running anywhere in the process subtree rooted
// at pid (the pane's foreground pid). A pane hosts at most one agent, so the
// first hit wins.
func subtreeAgent(pid int, children map[int][]int, table map[int]procInfo) agentKind {
	stack := []int{pid}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if k := table[cur].kind; k != agentNone {
			return k
		}
		stack = append(stack, children[cur]...)
	}
	return agentNone
}

// agentKindsByPane classifies every pane by the agent found in its process
// subtree, keyed by pane id.
func agentKindsByPane(panes []Pane, table map[int]procInfo) map[string]agentKind {
	children := map[int][]int{}
	for pid, info := range table {
		children[info.ppid] = append(children[info.ppid], pid)
	}
	kinds := map[string]agentKind{}
	for _, p := range panes {
		if k := subtreeAgent(p.PID, children, table); k != agentNone {
			kinds[p.ID] = k
		}
	}
	return kinds
}

// codexStatus reads Codex's state straight from its pane title — no capture
// needed: "Action Required" means it's blocked on approval, a leading braille
// spinner means it's working, otherwise it's idle.
func codexStatus(p Pane) agentStatus {
	if strings.Contains(p.Title, "Action Required") {
		return agentWaiting
	}
	runes := []rune(p.Title)
	if len(runes) > 0 && isSpinner(runes[0]) {
		return agentRunning
	}
	return agentStopped
}

// agentDisplayName is the label shown for an agent pane row: the task from the
// title when the agent advertises one, else the agent's plain name.
func agentDisplayName(kind agentKind, title string) string {
	switch kind {
	case agentClaude:
		if n, ok := claudeTitleName(title); ok && n != "" && n != "Claude Code" {
			return n
		}
		return "claude"
	case agentCodex:
		return codexName(title)
	}
	return ""
}

// codexName pulls a task label from a Codex title: a working title is
// "<spinner> <task>"; idle ("<dir>") and waiting ("… Action Required …") titles
// carry no task, so fall back to "codex".
func codexName(title string) string {
	runes := []rune(title)
	if len(runes) >= 2 && isSpinner(runes[0]) && runes[1] == ' ' {
		if t := strings.TrimSpace(string(runes[2:])); t != "" {
			return t
		}
	}
	return "codex"
}

const brailleSpinners = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠁⠂⠄⠈⠐⠠"

var waitingPattern = regexp.MustCompile(`❯ [0-9]+\.|❯ Allow|❯ Deny|❯ allow`)

func isSpinner(r rune) bool { return strings.ContainsRune(brailleSpinners, r) }

// claudeTitleName reports whether a pane title carries the Claude Code
// marker (✳ idle / braille spinner running), returning the agent's display
// name from the title when it does.
func claudeTitleName(title string) (string, bool) {
	runes := []rune(title)
	if len(runes) < 2 || (runes[0] != '✳' && !isSpinner(runes[0])) || runes[1] != ' ' {
		return "", false
	}
	return strings.TrimSpace(string(runes[2:])), true
}

// detectAgents returns agent statuses aggregated per session (ordered
// running > waiting > stopped), individually per pane id, and the agent kind
// per pane id (for icon rendering).
func detectAgents(panes []Pane) (map[string][]agentStatus, map[string]agentStatus, map[string]agentKind) {
	kinds := agentKindsByPane(panes, processTable())
	result := map[string][]agentStatus{}
	byPane := map[string]agentStatus{}
	type candidate struct {
		p    Pane
		kind agentKind
	}
	var candidates []candidate
	for _, p := range panes {
		if kind := kinds[p.ID]; kind != agentNone {
			candidates = append(candidates, candidate{p, kind})
		}
	}

	// capture-pane per idle Claude agent is the slow part; run them
	// concurrently. Codex reports its state via the title, so it's instant.
	statuses := make([]agentStatus, len(candidates))
	var fns []func()
	for i := range candidates {
		i := i
		fns = append(fns, func() {
			c := candidates[i]
			if c.kind == agentCodex {
				statuses[i] = codexStatus(c.p)
				return
			}
			if strings.ContainsAny(c.p.Title, brailleSpinners) {
				statuses[i] = agentRunning
				return
			}
			statuses[i] = agentStopped
			for _, line := range strings.Split(capturePane(c.p.ID, 15), "\n") {
				if strings.Contains(line, "⏵⏵") {
					continue // Claude Code statusline hints
				}
				if waitingPattern.MatchString(line) {
					statuses[i] = agentWaiting
					return
				}
			}
		})
	}
	parallel(fns...)
	for i, c := range candidates {
		result[c.p.Session] = append(result[c.p.Session], statuses[i])
		byPane[c.p.ID] = statuses[i]
	}
	for _, statuses := range result {
		sortAgentStatuses(statuses)
	}
	return result, byPane, kinds
}

func sortAgentStatuses(s []agentStatus) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] > s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
