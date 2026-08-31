# bmux

A neotree-style session navigator for tmux. bmux shows every repo you work
in, every git worktree inside it, and the tmux session attached to each —
one tree, docked on the left, with Claude Code agent statuses inline.

```
 bmux
▾ cockroach
  ▸● cockroach-primary ▶1  bdillmann/crdb-nodes-view
  ▸● cockroach-secondary ⏹1  crdb-65148
   ○ cockroach-mergebase  f0fc9e360
▾ goodhistogram
  ▸● goodhistogram  bdillmann/rust-port
```

- `●` worktree with a live session, `○` no session yet — press ⏎ and one is
  created on the spot.
- `▶2 ⏸1 ⏹3` — Claude Code agents in that session: running / waiting for
  input / stopped.
- Repos are discovered from live sessions, [roachdev](https://github.com/cockroachlabs/roachdev)
  worktrees (optional), and a persistent registry of every repo that has ever
  had a session (`~/.config/bmux/repos.json`).

## Install

```sh
go install github.com/angles-n-daemons/bmux@latest
```

Requires tmux ≥ 3.2 and git. If `roachdev` is on your PATH it is used for
worktree create/remove (submodule-aware); otherwise plain `git worktree` is
used, placing worktrees under `~/.worktrees` (override with
`BMUX_WORKTREE_DIR`).

## Wiring

Side panel on a key (`.tmux.conf`):

```tmux
bind e run-shell "~/go/bin/bmux toggle"
```

Boot everything from a bare `tmux` (`.zshrc` / aliases):

```sh
# bare `tmux` boots the session navigator; any args pass through untouched
tmux() { if [ $# -eq 0 ]; then ~/go/bin/bmux up; else command tmux "$@"; fi }
```

## Commands

| Command | What it does |
|---|---|
| `bmux` | Run the tree TUI in the current pane (inside tmux) |
| `bmux toggle` | Open/close the panel **globally**: one live pane that follows you across windows and sessions (state in the `@bmux_open` server option) |
| `bmux ensure` | Hook target: `join-pane`s the traveling panel into the window the client is looking at (respawns it if the process died). Wire it up: |
| `bmux up` | Start a detached session for **every** worktree of every known repo, plus a `home` session running the tree full-screen, then attach. Idempotent. |

```tmux
set-hook -g session-window-changed 'run-shell -b "~/go/bin/bmux ensure"'
set-hook -g client-session-changed 'run-shell -b "~/go/bin/bmux ensure"'
```

Because it is the same pane being moved (not a new process), cursor
position, fold state, and scroll all persist as the panel follows you.
Windows already showing bmux (the `home` full-screen tree) are skipped.
Quitting the panel with `q` closes it globally.

## Keys

| Key | Action |
|---|---|
| `j` / `k` | move |
| `⏎` | jump to session (creating it first if the worktree has none); on window/pane rows, jump straight there |
| `l` / `h` | expand / collapse (sessions expand into windows, windows into panes); `h` on a folded row jumps to its parent |
| `a` | create a worktree in the repo under the cursor (empty name = auto-generated), on a new branch named after it, with a session, and jump to it |
| `d` | delete: kills the session **and** removes the worktree after a `y/N` confirm. On a main checkout it only kills the session — the checkout itself is never touched. On an unpinnable repo header, removes it from the registry. |
| `g` / `G` | first / last row |
| `r` | refresh now (the tree also refreshes every 2s) |
| `?` | help |
| `q` / `esc` | close |

## Appearance

The panel is chrome, and chrome recedes: it keeps the surrounding scheme's
exact hues but desaturated and darkened, like an editor sidebar — so it
tracks focus like any other pane while reading as navigation, not content.
Defaults: `bg=#261724,fg=#9cc9ad` blurred, `bg=#152627,fg=#d4c39b` focused.
Override live — no rebuild, takes effect next time the panel opens:

```tmux
set -g @bmux_window_style 'bg=#261724,fg=#9cc9ad'         # blurred
set -g @bmux_window_active_style 'bg=#152627,fg=#d4c39b'  # focused
```

The panel opens at a quarter of the window width (at least 40 columns, at
most 40%). Override with a column count or percentage:

```tmux
set -g @bmux_width 80    # columns
set -g @bmux_width 30%   # of the window
```

A status command's first output line renders right-aligned in the panel
title bar, refreshed with the tree (make it cache itself if it's expensive):

```tmux
set -g @bmux_status_cmd '~/.tmux/scripts/claude-usage.sh'
```

## Notes

- Session ⇄ worktree matching is by path: a session belongs to a worktree
  when its `session_path` resolves to that worktree's root, regardless of the
  session's name.
- Worktrees whose directory was deleted without `git worktree prune` are
  ignored.
- Quitting the tree in the `home` session ends that session; with
  `detach-on-destroy on` (tmux default) this detaches you — i.e. `q` from
  home means "leave tmux".
- `BMUX_SOCKET=<name>` points bmux at `tmux -L <name>`, handy for testing
  against a scratch server.

## Agent status detection

Ported from [tmux-agent-statuses](https://github.com/angles-n-daemons/tmux-agent-statuses):
Claude Code sets pane titles to `✳ <name>` when idle and a braille spinner
while working; an idle pane whose recent lines show a permission prompt
(`❯ Allow` / `❯ Deny` / numbered options) counts as waiting.
