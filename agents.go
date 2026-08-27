package main

import (
	"regexp"
	"strings"
)

// Claude Code agent detection, ported from tmux-agent-statuses:
// pane titles start with ✳ (idle) or a braille spinner (working); an idle
// pane whose recent content shows a permission prompt is "waiting".

type agentStatus int

const (
	agentStopped agentStatus = iota
	agentWaiting
	agentRunning
)

const brailleSpinners = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠁⠂⠄⠈⠐⠠"

var waitingPattern = regexp.MustCompile(`❯ [0-9]+\.|❯ Allow|❯ Deny|❯ allow`)

var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true, "ksh": true,
}

func isSpinner(r rune) bool { return strings.ContainsRune(brailleSpinners, r) }

// detectAgents returns per-session agent statuses, ordered
// running > waiting > stopped within each session.
func detectAgents(panes []Pane) map[string][]agentStatus {
	result := map[string][]agentStatus{}
	var candidates []Pane
	for _, p := range panes {
		runes := []rune(p.Title)
		if len(runes) < 2 || (runes[0] != '✳' && !isSpinner(runes[0])) || runes[1] != ' ' {
			continue
		}
		if shellCommands[p.Command] {
			continue // agent exited, foreground reverted to a shell
		}
		candidates = append(candidates, p)
	}

	// capture-pane per idle agent is the slow part; run them concurrently.
	statuses := make([]agentStatus, len(candidates))
	var fns []func()
	for i := range candidates {
		i := i
		fns = append(fns, func() {
			p := candidates[i]
			if strings.ContainsAny(p.Title, brailleSpinners) {
				statuses[i] = agentRunning
				return
			}
			statuses[i] = agentStopped
			for _, line := range strings.Split(capturePane(p.ID, 15), "\n") {
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
	for i, p := range candidates {
		result[p.Session] = append(result[p.Session], statuses[i])
	}
	for _, statuses := range result {
		sortAgentStatuses(statuses)
	}
	return result
}

func sortAgentStatuses(s []agentStatus) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] > s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
