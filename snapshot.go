package main

import (
	"path/filepath"
	"sort"
	"sync"
)

// snapshot is one consistent view of the world: repos, worktrees, sessions,
// windows/panes, and agent statuses. Rebuilt on every refresh tick.

type wtRow struct {
	WT      Worktree
	Session *Session // nil when no session is running for this worktree
	Agents  []agentStatus
}

type sessRow struct { // extra session on an already-claimed worktree, or non-repo session
	Session Session
	Agents  []agentStatus
}

type repoGroup struct {
	Root      string // main repo root; "" for the (no repo) group
	Name      string
	Worktrees []wtRow
	Extras    []sessRow
}

type snapshot struct {
	Repos          []repoGroup
	Windows        map[string][]Window
	Panes          map[string][]Pane // keyed by session
	CurrentSession string
}

// gather builds a snapshot. It also upserts every live-session repo into the
// persistent registry so repos never vanish from the tree. discoveredRoots
// carries backend discovery (slow, e.g. roachdev) done once at startup
// rather than on every refresh tick.
func gather(discoveredRoots []string) snapshot {
	var (
		sessions []Session
		allPanes []Pane
		windows  map[string][]Window
		current  string
	)
	parallel(
		func() { sessions, _ = listSessions() },
		func() { allPanes, _ = listAllPanes() },
		func() { windows, _ = listAllWindows() },
		func() { current = currentClientSession() },
	)

	reg := loadRegistry()

	// Resolve each session's worktree toplevel concurrently.
	tops := make([]string, len(sessions))
	sessionRootsIdx := make([]string, len(sessions))
	var fns []func()
	for i := range sessions {
		i := i
		fns = append(fns, func() {
			tops[i] = repoToplevel(sessions[i].Path)
			if tops[i] != "" {
				sessionRootsIdx[i] = repoMainRoot(tops[i])
			}
		})
	}
	parallel(fns...)

	toplevels := map[string]string{} // session name -> worktree toplevel ("" = not a repo)
	var sessionRoots []string
	roots := map[string]bool{}
	for i, s := range sessions {
		toplevels[s.Name] = tops[i]
		if root := sessionRootsIdx[i]; root != "" {
			roots[root] = true
			sessionRoots = append(sessionRoots, root)
		}
	}
	reg.upsert(sessionRoots)
	for _, root := range discoveredRoots {
		roots[root] = true
	}
	for _, root := range reg.Repos {
		roots[root] = true
	}

	// Sessions grouped by the worktree they live in, alphabetical within.
	byToplevel := map[string][]Session{}
	for _, s := range sessions {
		byToplevel[toplevels[s.Name]] = append(byToplevel[toplevels[s.Name]], s)
	}
	for _, ss := range byToplevel {
		sort.Slice(ss, func(i, j int) bool { return ss[i].Name < ss[j].Name })
	}

	agents := detectAgents(allPanes)
	panesBySession := map[string][]Pane{}
	for _, p := range allPanes {
		if p.Command == "bmux" {
			continue // never show ourselves (panel/home tree) in the tree
		}
		panesBySession[p.Session] = append(panesBySession[p.Session], p)
	}

	// List every repo's worktrees concurrently.
	rootList := make([]string, 0, len(roots))
	for root := range roots {
		rootList = append(rootList, root)
	}
	wtsByRoot := make([][]Worktree, len(rootList))
	fns = fns[:0]
	for i := range rootList {
		i := i
		fns = append(fns, func() {
			if repoToplevel(rootList[i]) == "" {
				return // repo moved/deleted; keep in registry but don't render
			}
			wtsByRoot[i] = repoWorktrees(rootList[i])
		})
	}
	parallel(fns...)

	var groups []repoGroup
	for i, root := range rootList {
		if len(wtsByRoot[i]) == 0 {
			continue
		}
		g := repoGroup{Root: root, Name: filepath.Base(root)}
		for _, wt := range wtsByRoot[i] {
			row := wtRow{WT: wt}
			matched := byToplevel[wt.Path]
			if len(matched) > 0 {
				s := matched[0]
				row.Session = &s
				row.Agents = agents[s.Name]
				for _, extra := range matched[1:] {
					g.Extras = append(g.Extras, sessRow{Session: extra, Agents: agents[extra.Name]})
				}
				delete(byToplevel, wt.Path)
			}
			g.Worktrees = append(g.Worktrees, row)
		}
		sort.Slice(g.Worktrees, func(i, j int) bool {
			// Main worktree first, then linked worktrees alphabetically.
			if g.Worktrees[i].WT.IsMain != g.Worktrees[j].WT.IsMain {
				return g.Worktrees[i].WT.IsMain
			}
			return filepath.Base(g.Worktrees[i].WT.Path) < filepath.Base(g.Worktrees[j].WT.Path)
		})
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })

	// Sessions in non-repo directories.
	if nogit := byToplevel[""]; len(nogit) > 0 {
		g := repoGroup{Root: "", Name: "(no repo)"}
		for _, s := range nogit {
			g.Extras = append(g.Extras, sessRow{Session: s, Agents: agents[s.Name]})
		}
		groups = append(groups, g)
	}

	snap := snapshot{
		Repos:          groups,
		Windows:        windows,
		Panes:          panesBySession,
		CurrentSession: current,
	}
	saveSnapshotCache(snap)
	return snap
}

func parallel(fns ...func()) {
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			f()
		}(fn)
	}
	wg.Wait()
}
