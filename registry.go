package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// The registry remembers every repo that has ever had a tmux session, so
// repos stay listed (and jumpable) after their last session dies.

type registry struct {
	Repos []string `json:"repos"`
}

func registryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "bmux", "repos.json")
}

func loadRegistry() registry {
	var r registry
	p := registryPath()
	if p == "" {
		return r
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return r
	}
	_ = json.Unmarshal(data, &r)
	return r
}

func (r registry) save() {
	p := registryPath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	sort.Strings(r.Repos)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, append(data, '\n'), 0o644)
}

func (r registry) has(root string) bool {
	for _, x := range r.Repos {
		if x == root {
			return true
		}
	}
	return false
}

// upsert adds roots not yet present and persists if anything changed.
func (r *registry) upsert(roots []string) {
	changed := false
	for _, root := range roots {
		if root != "" && !r.has(root) {
			r.Repos = append(r.Repos, root)
			changed = true
		}
	}
	if changed {
		r.save()
	}
}

func (r *registry) remove(root string) {
	out := r.Repos[:0]
	for _, x := range r.Repos {
		if x != root {
			out = append(out, x)
		}
	}
	r.Repos = out
	r.save()
}
