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
	for _, p := range panes {
		runes := []rune(p.Title)
		if len(runes) < 2 || (runes[0] != '✳' && !isSpinner(runes[0])) || runes[1] != ' ' {
			continue
		}
		if shellCommands[p.Command] {
			continue // agent exited, foreground reverted to a shell
		}
		status := agentStopped
		if strings.ContainsAny(p.Title, brailleSpinners) {
			status = agentRunning
		} else {
			content := capturePane(p.ID, 15)
			for _, line := range strings.Split(content, "\n") {
				if strings.Contains(line, "⏵⏵") {
					continue // Claude Code statusline hints
				}
				if waitingPattern.MatchString(line) {
					status = agentWaiting
					break
				}
			}
		}
		result[p.Session] = append(result[p.Session], status)
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
