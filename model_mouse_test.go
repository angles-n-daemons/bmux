package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseWheelMovesCursorWithinRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := model{
		rows:     []row{{Key: "first"}, {Key: "second"}, {Key: "third"}},
		cursor:   1,
		mode:     modeNormal,
		expanded: map[string]bool{},
	}

	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	m = updated.(model)
	if m.cursor != 2 {
		t.Fatalf("wheel down cursor = %d, want 2", m.cursor)
	}

	updated, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	m = updated.(model)
	if m.cursor != 2 {
		t.Fatalf("wheel down past last row moved cursor to %d", m.cursor)
	}

	updated, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	m = updated.(model)
	if m.cursor != 1 {
		t.Fatalf("wheel up cursor = %d, want 1", m.cursor)
	}
}

func TestMouseClickUsesScrolledRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := model{
		snap: snapshot{Repos: []repoGroup{
			{Root: "/a", Name: "a"},
			{Root: "/b", Name: "b"},
			{Root: "/c", Name: "c"},
			{Root: "/d", Name: "d"},
			{Root: "/e", Name: "e"},
			{Root: "/f", Name: "f"},
		}},
		cursor:   4,
		height:   5,
		mode:     modeNormal,
		expanded: map[string]bool{},
	}
	m.rebuildRows()

	// With three visible rows and the cursor on row 4, row 2 is at the top
	// of the viewport. Clicking its screen line should fold repo c.
	updated, _ := m.Update(tea.MouseMsg{
		Y:      1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if m.cursor != 2 {
		t.Fatalf("clicked cursor = %d, want 2", m.cursor)
	}
	if expanded, ok := m.expanded["r:/cc"]; !ok || expanded {
		t.Fatalf("clicked repo expansion = %v, present = %v; want explicit false", expanded, ok)
	}
}
