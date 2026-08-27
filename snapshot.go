package main

import (
	"path/filepath"
	"sort"
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
// persistent registry so repos never vanish from the tree.
func gather(backend worktreeBackend) snapshot {
	sessions, _ := listSessions()
	reg := loadRegistry()

	toplevels := map[string]string{} // session name -> worktree toplevel ("" = not a repo)
	var sessionRoots []string
	roots := map[string]bool{}
	for _, s := range sessions {
		top := repoToplevel(s.Path)
		toplevels[s.Name] = top
		if top != "" {
			root := repoMainRoot(top)
			roots[root] = true
			sessionRoots = append(sessionRoots, root)
		}
	}
	reg.upsert(sessionRoots)
	for _, root := range backend.DiscoverRoots() {
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

	allPanes, _ := listAllPanes()
	agents := detectAgents(allPanes)
	windows, _ := listAllWindows()
	panesBySession := map[string][]Pane{}
	for _, p := range allPanes {
		panesBySession[p.Session] = append(panesBySession[p.Session], p)
	}

	var groups []repoGroup
	for root := range roots {
		if repoToplevel(root) == "" {
			continue // repo moved/deleted; keep in registry but don't render
		}
		g := repoGroup{Root: root, Name: filepath.Base(root)}
		for _, wt := range repoWorktrees(root) {
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

	return snapshot{
		Repos:          groups,
		Windows:        windows,
		Panes:          panesBySession,
		CurrentSession: currentClientSession(),
	}
}
