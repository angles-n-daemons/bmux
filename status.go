package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	statusInterval = 30 * time.Second
	statusTimeout  = 30 * time.Second
)

// runStatusCmds runs the user's title-bar status commands. These can be
// expensive (e.g. `roachdev claude usage` saturates several cores), so they
// run one after the other under a lock shared by every bmux process: two
// panels never run them concurrently, and a waiter typically hits whatever
// cache the script keeps once the lock holder has refreshed it.
func runStatusCmds() (claude, codex string) {
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "bmux-status.lock"), os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer f.Close() // releases the flock
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	return statusFirstLine("@bmux_status_cmd"), statusFirstLine("@bmux_status_codex_cmd")
}

func statusFirstLine(opt string) string {
	cmd := userOption(opt, "")
	if cmd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	// The shell's children (e.g. a pipeline) inherit stdout; don't block on
	// them after the shell is killed.
	c.WaitDelay = time.Second
	out, _ := c.Output()
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}
