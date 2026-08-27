package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sep splits tmux -F output fields. It must be printable: tmux octal-escapes
// non-printable bytes (\x1f becomes the literal text `\037`) in its output.
// Fields that may contain arbitrary text (pane titles) go last, under SplitN.
const sep = "\t"

type Session struct {
	Name string
	Path string
}

type Window struct {
	Session string
	Index   int
	Name    string
	Active  bool
}

type Pane struct {
	Session string
	Window  int
	Index   int
	ID      string
	Title   string
	Command string
	Active  bool
}

// tmuxCmd builds a tmux invocation, honoring BMUX_SOCKET for scratch-server
// testing (`BMUX_SOCKET=navtest bmux up`).
func tmuxCmd(args ...string) *exec.Cmd {
	if sock := os.Getenv("BMUX_SOCKET"); sock != "" {
		args = append([]string{"-L", sock}, args...)
	}
	return exec.Command("tmux", args...)
}

func tmuxOut(args ...string) (string, error) {
	cmd := tmuxCmd(args...)
	var errb strings.Builder
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return string(out), nil
}

func tmuxRun(args ...string) error {
	_, err := tmuxOut(args...)
	return err
}

func insideTmux() bool { return os.Getenv("TMUX") != "" }

func listSessions() ([]Session, error) {
	out, err := tmuxOut("list-sessions", "-F", "#{session_name}"+sep+"#{session_path}")
	if err != nil {
		// No server running reads as an error; treat as no sessions.
		return nil, nil
	}
	var ss []Session
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 {
			ss = append(ss, Session{Name: parts[0], Path: parts[1]})
		}
	}
	return ss, nil
}

func listAllWindows() (map[string][]Window, error) {
	out, err := tmuxOut("list-windows", "-a", "-F",
		"#{session_name}"+sep+"#{window_index}"+sep+"#{window_name}"+sep+"#{?window_active,1,0}")
	if err != nil {
		return nil, err
	}
	m := map[string][]Window{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		p := strings.SplitN(line, sep, 4)
		if len(p) != 4 {
			continue
		}
		idx, _ := strconv.Atoi(p[1])
		m[p[0]] = append(m[p[0]], Window{Session: p[0], Index: idx, Name: p[2], Active: p[3] == "1"})
	}
	return m, nil
}

func listAllPanes() ([]Pane, error) {
	out, err := tmuxOut("list-panes", "-a", "-F",
		"#{session_name}"+sep+"#{window_index}"+sep+"#{pane_index}"+sep+"#{pane_id}"+sep+
			"#{pane_current_command}"+sep+"#{?pane_active,1,0}"+sep+"#{pane_title}")
	if err != nil {
		return nil, err
	}
	var ps []Pane
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		p := strings.SplitN(line, sep, 7)
		if len(p) != 7 {
			continue
		}
		widx, _ := strconv.Atoi(p[1])
		pidx, _ := strconv.Atoi(p[2])
		ps = append(ps, Pane{
			Session: p[0], Window: widx, Index: pidx,
			ID: p[3], Command: p[4], Active: p[5] == "1", Title: p[6],
		})
	}
	return ps, nil
}

func capturePane(paneID string, lines int) string {
	out, err := tmuxOut("capture-pane", "-p", "-t", paneID, "-S", strconv.Itoa(-lines))
	if err != nil {
		return ""
	}
	return out
}

func currentClientSession() string {
	out, err := tmuxOut("display-message", "-p", "#{client_session}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func hasSession(name string) bool {
	return tmuxRun("has-session", "-t", "="+name) == nil
}

func newSession(name, path string, command ...string) error {
	args := []string{"new-session", "-d", "-s", name}
	if path != "" {
		args = append(args, "-c", path)
	}
	args = append(args, command...)
	return tmuxRun(args...)
}

func switchClient(session string) error {
	return tmuxRun("switch-client", "-t", "="+session)
}

func killSession(name string) error {
	return tmuxRun("kill-session", "-t", "="+name)
}

func selectWindow(session string, window int) error {
	return tmuxRun("select-window", "-t", fmt.Sprintf("=%s:%d", session, window))
}

func selectPane(session string, window, pane int) error {
	return tmuxRun("select-pane", "-t", fmt.Sprintf("=%s:%d.%d", session, window, pane))
}

// sanitizeSessionName strips characters tmux forbids in session names.
func sanitizeSessionName(name string) string {
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, ":", "-")
	if name == "" {
		name = "session"
	}
	return name
}

// uniqueSessionName appends -2, -3, ... until the name is free.
func uniqueSessionName(base string) string {
	name := sanitizeSessionName(base)
	if !hasSession(name) {
		return name
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", name, i)
		if !hasSession(cand) {
			return cand
		}
	}
}
