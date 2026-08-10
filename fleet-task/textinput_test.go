package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func fieldKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestFormModelSpaceInsertsLiteralSpace(t *testing.T) {
	m := newFormModel([]string{"Description"})

	for _, r := range "fix bug" {
		var msg tea.KeyMsg
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace}
		} else {
			msg = fieldKey(r)
		}
		m2, _ := m.Update(msg)
		m = m2.(formModel)
	}

	if got := string(m.fields[0].value); got != "fix bug" {
		t.Fatalf("value = %q, want %q", got, "fix bug")
	}
}

func TestFormModelTabAndShiftTabMoveFocus(t *testing.T) {
	m := newFormModel([]string{"Ticket", "Description"})

	if m.focus != 0 {
		t.Fatalf("initial focus = %d, want 0", m.focus)
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(formModel)
	if m.focus != 1 {
		t.Fatalf("after tab: focus = %d, want 1", m.focus)
	}

	// Tab wraps back to the first field.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(formModel)
	if m.focus != 0 {
		t.Fatalf("after wrap tab: focus = %d, want 0", m.focus)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = m2.(formModel)
	if m.focus != 1 {
		t.Fatalf("after shift+tab wrap: focus = %d, want 1", m.focus)
	}
}

func TestFormModelUpDownAlsoMoveFocus(t *testing.T) {
	m := newFormModel([]string{"Ticket", "Description"})

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(formModel)
	if m.focus != 1 {
		t.Fatalf("after down: focus = %d, want 1", m.focus)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = m2.(formModel)
	if m.focus != 0 {
		t.Fatalf("after up: focus = %d, want 0", m.focus)
	}
}

func TestFormModelEnterAdvancesThenSubmits(t *testing.T) {
	m := newFormModel([]string{"Ticket", "Description"})

	for _, r := range "PROJ-1" {
		m2, _ := m.Update(fieldKey(r))
		m = m2.(formModel)
	}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(formModel)
	if m.done {
		t.Fatal("enter on non-last field should not finish the form")
	}
	if m.focus != 1 {
		t.Fatalf("after enter on first field: focus = %d, want 1", m.focus)
	}
	if cmd != nil {
		t.Fatal("enter on non-last field should not quit")
	}

	for _, r := range "fix it" {
		var msg tea.KeyMsg
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace}
		} else {
			msg = fieldKey(r)
		}
		m2, _ = m.Update(msg)
		m = m2.(formModel)
	}

	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(formModel)
	if !m.done {
		t.Fatal("enter on last field should finish the form")
	}
	if cmd == nil {
		t.Fatal("enter on last field should return tea.Quit")
	}

	if got := string(m.fields[0].value); got != "PROJ-1" {
		t.Errorf("fields[0] = %q, want PROJ-1", got)
	}
	if got := string(m.fields[1].value); got != "fix it" {
		t.Errorf("fields[1] = %q, want %q", got, "fix it")
	}
}

func TestFormModelCancel(t *testing.T) {
	m := newFormModel([]string{"Ticket"})

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(formModel)
	if !m.cancelled || !m.done {
		t.Fatal("esc should cancel and finish the form")
	}
}

func TestFormModelCursorEditing(t *testing.T) {
	m := newFormModel([]string{"Ticket"})

	for _, r := range "abd" {
		m2, _ := m.Update(fieldKey(r))
		m = m2.(formModel)
	}
	// Move left once and insert "c" between "b" and "d".
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = m2.(formModel)
	m2, _ = m.Update(fieldKey('c'))
	m = m2.(formModel)

	if got := string(m.fields[0].value); got != "abcd" {
		t.Fatalf("value = %q, want abcd", got)
	}
}
