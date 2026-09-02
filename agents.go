package main

import (
	"regexp"
	"strings"
)

// Coding-agent detection. Two agents are recognised:
//
//   - Claude Code, ported from tmux-agent-statuses: pane titles start with ✳
//     (idle) or a braille spinner (working); an idle pane whose recent content
//     shows a permission prompt is "waiting".
//   - Codex: identified by its foreground command ("codex"). It carries its
//     state in the pane title — a leading braille spinner while working and the
//     literal "Action Required" when it needs approval.

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

// agentOf identifies the coding agent running in a pane, returning its kind and
// a display name. Codex is checked first: while it works its title also carries
// a braille spinner, which would otherwise look like Claude's marker.
func agentOf(p Pane) (agentKind, string) {
	if p.Command == "codex" {
		return agentCodex, "codex"
	}
	if name, ok := claudeTitleName(p.Title); ok && !shellCommands[p.Command] {
		if name == "" || name == "Claude Code" {
			name = "claude"
		}
		return agentClaude, name
	}
	return agentNone, ""
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

const brailleSpinners = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠁⠂⠄⠈⠐⠠"

var waitingPattern = regexp.MustCompile(`❯ [0-9]+\.|❯ Allow|❯ Deny|❯ allow`)

var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true, "ksh": true,
}

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
// running > waiting > stopped) and individually per pane id.
func detectAgents(panes []Pane) (map[string][]agentStatus, map[string]agentStatus) {
	result := map[string][]agentStatus{}
	byPane := map[string]agentStatus{}
	type candidate struct {
		p    Pane
		kind agentKind
	}
	var candidates []candidate
	for _, p := range panes {
		if kind, _ := agentOf(p); kind != agentNone {
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
	return result, byPane
}

func sortAgentStatuses(s []agentStatus) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] > s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
