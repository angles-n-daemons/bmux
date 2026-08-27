package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// The snapshot cache makes the first paint instant: the tree renders from
// the previous run's data immediately, then the live refresh replaces it.

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sock := os.Getenv("BMUX_SOCKET")
	if sock == "" {
		sock = "default"
	}
	return filepath.Join(dir, "bmux", "snapshot-"+sock+".json")
}

// uiState survives panel close/reopen: the cursor row and fold state are
// restored on the next open. Saved eagerly on every change because the
// toggle closes the panel with kill-pane — there is no clean-exit hook.
type uiState struct {
	CursorKey string
	Expanded  map[string]bool
}

func uiStatePath() string {
	p := cachePath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "state-"+filepath.Base(p))
}

func loadUIState() (uiState, bool) {
	var st uiState
	p := uiStatePath()
	if p == "" {
		return st, false
	}
	data, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(data, &st) != nil {
		return st, false
	}
	return st, true
}

func saveUIState(st uiState) {
	p := uiStatePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

func loadSnapshotCache() (snapshot, bool) {
	var snap snapshot
	p := cachePath()
	if p == "" {
		return snap, false
	}
	data, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(data, &snap) != nil {
		return snap, false
	}
	return snap, len(snap.Repos) > 0
}

func saveSnapshotCache(snap snapshot) {
	p := cachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}
