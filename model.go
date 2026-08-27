package main

import (
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type rowKind int

const (
	rowRepo rowKind = iota
	rowWorktree
	rowSession // extra session (same worktree twice, or non-repo session)
	rowWindow
	rowPane
)

type row struct {
	Kind  rowKind
	Key   string // stable identity across refreshes
	Depth int

	Repo *repoGroup
	WT   *wtRow
	Sess *sessRow
	Win  *Window
	Pane *Pane
}

// sessionName returns the session behind this row, if any.
func (r row) sessionName() string {
	switch {
	case r.Kind == rowWorktree && r.WT.Session != nil:
		return r.WT.Session.Name
	case r.Kind == rowSession:
		return r.Sess.Session.Name
	case r.Kind == rowWindow:
		return r.Win.Session
	case r.Kind == rowPane:
		return r.Pane.Session
	}
	return ""
}

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeBusy
)

type (
	tickMsg       struct{}
	dataMsg       struct{ snap snapshot }
	actionDoneMsg struct {
		err    error
		notice string
	}
)

type model struct {
	backend worktreeBackend
	snap    snapshot
	rows    []row
	cursor  int
	offset  int
	// expanded overrides the default fold state (repos open, rest closed).
	expanded map[string]bool

	mode       mode
	promptRepo string // main root the pending `a` applies to
	input      string
	confirmMsg string
	confirmFn  func() tea.Msg
	busyMsg    string
	footer     string

	width, height int
}

func newModel(backend worktreeBackend) model {
	return model{backend: backend, expanded: map[string]bool{}, width: 34, height: 24}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) refreshCmd() tea.Cmd {
	backend := m.backend
	return func() tea.Msg { return dataMsg{snap: gather(backend)} }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		if m.mode == modeBusy {
			return m, tickCmd() // don't rebuild under a running action
		}
		return m, tea.Batch(m.refreshCmd(), tickCmd())
	case dataMsg:
		m.snap = msg.snap
		m.rebuildRows()
		return m, nil
	case actionDoneMsg:
		m.mode = modeNormal
		if msg.err != nil {
			m.footer = "✗ " + msg.err.Error()
		} else {
			m.footer = msg.notice
		}
		return m, m.refreshCmd()
	case tea.KeyMsg:
		switch m.mode {
		case modePrompt:
			return m.updatePrompt(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeBusy:
			return m, nil
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.footer = ""
	// Rapid input coalesces into one KeyRunes message ("jjjl"); apply each
	// rune in order, stopping if a key switches us out of normal mode.
	if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) > 1 {
		var cmds []tea.Cmd
		cur := m
		for _, r := range msg.Runes {
			if cur.mode != modeNormal {
				break
			}
			next, cmd := cur.applyNormalKey(string(r))
			cur = next.(model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return cur, tea.Batch(cmds...)
	}
	return m.applyNormalKey(msg.String())
}

func (m model) applyNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(m.rows) - 1
	case "r":
		return m, m.refreshCmd()
	case "enter":
		return m.enterRow()
	case "l":
		return m.expandRow()
	case "h":
		return m.collapseRow()
	case "a":
		return m.startCreate()
	case "d":
		return m.startDelete()
	}
	return m, nil
}

func (m *model) cur() *row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

// --- fold state ---

func (m model) isExpanded(r row) bool {
	if v, ok := m.expanded[r.Key]; ok {
		return v
	}
	return r.Kind == rowRepo // repos default open, everything else closed
}

func (r row) expandable() bool {
	switch r.Kind {
	case rowRepo:
		return true
	case rowWorktree:
		return r.WT.Session != nil
	case rowSession, rowWindow:
		return true
	}
	return false
}

// --- key actions ---

func (m model) enterRow() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil {
		return m, nil
	}
	switch r.Kind {
	case rowRepo:
		m.expanded[r.Key] = !m.isExpanded(*r)
		m.rebuildRows()
		return m, nil
	case rowWorktree:
		if r.WT.Session != nil {
			return m, jumpCmd(r.WT.Session.Name, -1, -1)
		}
		wt := r.WT.WT
		return m, func() tea.Msg {
			name := uniqueSessionName(filepath.Base(wt.Path))
			if err := newSession(name, wt.Path); err != nil {
				return actionDoneMsg{err: err}
			}
			if err := switchClient(name); err != nil {
				return actionDoneMsg{err: err}
			}
			return actionDoneMsg{notice: "session " + name + " started"}
		}
	case rowSession:
		return m, jumpCmd(r.Sess.Session.Name, -1, -1)
	case rowWindow:
		return m, jumpCmd(r.Win.Session, r.Win.Index, -1)
	case rowPane:
		return m, jumpCmd(r.Pane.Session, r.Pane.Window, r.Pane.Index)
	}
	return m, nil
}

func jumpCmd(session string, window, pane int) tea.Cmd {
	return func() tea.Msg {
		if window >= 0 {
			if err := selectWindow(session, window); err != nil {
				return actionDoneMsg{err: err}
			}
		}
		if pane >= 0 {
			if err := selectPane(session, window, pane); err != nil {
				return actionDoneMsg{err: err}
			}
		}
		if err := switchClient(session); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{}
	}
}

func (m model) expandRow() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil || !r.expandable() {
		return m, nil
	}
	if !m.isExpanded(*r) {
		m.expanded[r.Key] = true
		m.rebuildRows()
	}
	return m, nil
}

// collapseRow folds the node under the cursor; if already folded (or not
// foldable), the cursor jumps to the parent row — neotree behavior.
func (m model) collapseRow() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil {
		return m, nil
	}
	if r.expandable() && m.isExpanded(*r) {
		m.expanded[r.Key] = false
		m.rebuildRows()
		return m, nil
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].Depth < r.Depth {
			m.cursor = i
			break
		}
	}
	return m, nil
}

func (m model) startCreate() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil || r.Repo == nil || r.Repo.Root == "" {
		m.footer = "a: move onto a repo first"
		return m, nil
	}
	m.mode = modePrompt
	m.promptRepo = r.Repo.Root
	m.input = ""
	return m, nil
}

func (m model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeNormal
		return m, nil
	case "enter":
		name := m.input
		if err := m.validateName(name); err != nil {
			m.footer = "✗ " + err.Error()
			m.mode = modeNormal
			return m, nil
		}
		root := m.promptRepo
		backend := m.backend
		m.mode = modeBusy
		m.busyMsg = "creating worktree…"
		return m, func() tea.Msg {
			path, err := backend.Create(root, name)
			if err != nil {
				return actionDoneMsg{err: err}
			}
			sess := uniqueSessionName(filepath.Base(path))
			if err := newSession(sess, path); err != nil {
				return actionDoneMsg{err: err}
			}
			if err := switchClient(sess); err != nil {
				return actionDoneMsg{err: err}
			}
			return actionDoneMsg{notice: "created " + filepath.Base(path)}
		}
	case "backspace":
		if len(m.input) > 0 {
			rs := []rune(m.input)
			m.input = string(rs[:len(rs)-1])
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.input += string(msg.Runes)
	}
	return m, nil
}

// validateName rejects names colliding with existing worktrees or sessions.
func (m model) validateName(name string) error {
	if name == "" {
		return nil // backend auto-generates
	}
	for _, g := range m.snap.Repos {
		for _, w := range g.Worktrees {
			base := filepath.Base(w.WT.Path)
			if base == name || base == filepath.Base(g.Root)+"-"+name {
				return fmt.Errorf("worktree %q already exists", base)
			}
		}
	}
	if hasSession(name) {
		return fmt.Errorf("session %q already exists", name)
	}
	return nil
}

func (m model) startDelete() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil {
		return m, nil
	}
	current := m.snap.CurrentSession
	otherSession := "" // somewhere to land if we kill the session we're in
	for _, g := range m.snap.Repos {
		for _, w := range g.Worktrees {
			if w.Session != nil && w.Session.Name != r.sessionName() {
				otherSession = w.Session.Name
			}
		}
		for _, s := range g.Extras {
			if s.Session.Name != r.sessionName() {
				otherSession = s.Session.Name
			}
		}
	}

	killSess := func(name string) error {
		if name == current && otherSession != "" {
			if err := switchClient(otherSession); err != nil {
				return err
			}
		}
		return killSession(name)
	}

	switch r.Kind {
	case rowRepo:
		g := r.Repo
		if g.Root == "" {
			return m, nil
		}
		for _, w := range g.Worktrees {
			if w.Session != nil {
				m.footer = "d: repo has live sessions"
				return m, nil
			}
		}
		root := g.Root
		m.mode = modeConfirm
		m.confirmMsg = fmt.Sprintf("unpin %s from bmux?", g.Name)
		m.confirmFn = func() tea.Msg {
			reg := loadRegistry()
			reg.remove(root)
			return actionDoneMsg{notice: "unpinned " + filepath.Base(root)}
		}
	case rowWorktree:
		w := r.WT
		name := filepath.Base(w.WT.Path)
		switch {
		case w.WT.IsMain && w.Session == nil:
			m.footer = "d: main checkout, nothing to do"
			return m, nil
		case w.WT.IsMain:
			sess := w.Session.Name
			m.mode = modeConfirm
			m.confirmMsg = fmt.Sprintf("kill session %s? (main checkout is never removed)", sess)
			m.confirmFn = func() tea.Msg {
				if err := killSess(sess); err != nil {
					return actionDoneMsg{err: err}
				}
				return actionDoneMsg{notice: "killed " + sess}
			}
		default:
			root, path := r.Repo.Root, w.WT.Path
			sess := ""
			if w.Session != nil {
				sess = w.Session.Name
			}
			backend := m.backend
			m.mode = modeConfirm
			if sess != "" {
				m.confirmMsg = fmt.Sprintf("kill session %s AND remove worktree %s?", sess, name)
			} else {
				m.confirmMsg = fmt.Sprintf("remove worktree %s?", name)
			}
			m.confirmFn = func() tea.Msg {
				if sess != "" {
					if err := killSess(sess); err != nil {
						return actionDoneMsg{err: err}
					}
				}
				if err := backend.Remove(root, path, name); err != nil {
					return actionDoneMsg{err: err}
				}
				return actionDoneMsg{notice: "removed " + name}
			}
		}
	case rowSession:
		sess := r.Sess.Session.Name
		m.mode = modeConfirm
		m.confirmMsg = fmt.Sprintf("kill session %s?", sess)
		m.confirmFn = func() tea.Msg {
			if err := killSess(sess); err != nil {
				return actionDoneMsg{err: err}
			}
			return actionDoneMsg{notice: "killed " + sess}
		}
	default:
		m.footer = "d: only sessions/worktrees can be deleted"
		return m, nil
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		fn := m.confirmFn
		m.mode = modeBusy
		m.busyMsg = "working…"
		return m, func() tea.Msg { return fn() }
	default:
		m.mode = modeNormal
		m.footer = "cancelled"
		return m, nil
	}
}

// --- row flattening ---

func (m *model) rebuildRows() {
	var curKey string
	if r := m.cur(); r != nil {
		curKey = r.Key
	}
	m.rows = m.rows[:0]

	for gi := range m.snap.Repos {
		g := &m.snap.Repos[gi]
		repoRow := row{Kind: rowRepo, Key: "r:" + g.Root + g.Name, Depth: 0, Repo: g}
		m.rows = append(m.rows, repoRow)
		if !m.isExpanded(repoRow) {
			continue
		}
		for wi := range g.Worktrees {
			w := &g.Worktrees[wi]
			wr := row{Kind: rowWorktree, Key: "w:" + w.WT.Path, Depth: 1, Repo: g, WT: w}
			m.rows = append(m.rows, wr)
			if w.Session != nil && m.isExpanded(wr) {
				m.appendSessionChildren(w.Session.Name, 2)
			}
		}
		for si := range g.Extras {
			s := &g.Extras[si]
			sr := row{Kind: rowSession, Key: "s:" + s.Session.Name, Depth: 1, Repo: g, Sess: s}
			m.rows = append(m.rows, sr)
			if m.isExpanded(sr) {
				m.appendSessionChildren(s.Session.Name, 2)
			}
		}
	}

	m.cursor = 0
	for i, r := range m.rows {
		if r.Key == curKey {
			m.cursor = i
			break
		}
	}
}

func (m *model) appendSessionChildren(session string, depth int) {
	panesByWindow := map[int][]Pane{}
	for _, p := range m.snap.Panes[session] {
		panesByWindow[p.Window] = append(panesByWindow[p.Window], p)
	}
	for _, w := range m.snap.Windows[session] {
		win := w
		wr := row{Kind: rowWindow, Key: fmt.Sprintf("win:%s:%d", session, w.Index), Depth: depth, Win: &win}
		m.rows = append(m.rows, wr)
		if m.isExpanded(wr) {
			for _, p := range panesByWindow[w.Index] {
				pane := p
				m.rows = append(m.rows, row{
					Kind:  rowPane,
					Key:   fmt.Sprintf("p:%s:%d:%d", session, p.Window, p.Index),
					Depth: depth + 1, Pane: &pane,
				})
			}
		}
	}
}
