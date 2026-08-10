package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestSelectModelVimNavigation(t *testing.T) {
	m := newSelectModel("", []string{"a", "b", "c"}, false, nil, true)

	m2, _ := m.Update(key('j'))
	m = m2.(selectModel)
	if m.cursor != 1 {
		t.Fatalf("after j: cursor = %d, want 1", m.cursor)
	}

	m2, _ = m.Update(key('j'))
	m = m2.(selectModel)
	if m.cursor != 2 {
		t.Fatalf("after jj: cursor = %d, want 2", m.cursor)
	}

	// j at the bottom should not overshoot.
	m2, _ = m.Update(key('j'))
	m = m2.(selectModel)
	if m.cursor != 2 {
		t.Fatalf("j at bottom: cursor = %d, want 2", m.cursor)
	}

	m2, _ = m.Update(key('k'))
	m = m2.(selectModel)
	if m.cursor != 1 {
		t.Fatalf("after k: cursor = %d, want 1", m.cursor)
	}

	m2, _ = m.Update(key('G'))
	m = m2.(selectModel)
	if m.cursor != 2 {
		t.Fatalf("after G: cursor = %d, want 2", m.cursor)
	}

	m2, _ = m.Update(key('g'))
	m = m2.(selectModel)
	m2, _ = m.Update(key('g'))
	m = m2.(selectModel)
	if m.cursor != 0 {
		t.Fatalf("after gg: cursor = %d, want 0", m.cursor)
	}
}

func TestSelectModelSingleEnterConfirmsCursor(t *testing.T) {
	m := newSelectModel("", []string{"a", "b", "c"}, false, nil, true)
	m2, _ := m.Update(key('j'))
	m = m2.(selectModel)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if !m.done || m.cancelled {
		t.Fatalf("expected done && !cancelled, got done=%v cancelled=%v", m.done, m.cancelled)
	}
	if len(m.confirmed) != 1 || m.confirmed[0] != "b" {
		t.Fatalf("confirmed = %v, want [b]", m.confirmed)
	}
}

func TestSelectModelMultiToggleAndEnter(t *testing.T) {
	m := newSelectModel("", []string{"a", "b", "c"}, true, nil, true)

	// Space toggles the item under the cursor and advances to the next row,
	// so checking off consecutive items is just repeated space presses.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(selectModel)
	if !m.checked["a"] {
		t.Fatalf("expected 'a' checked after space")
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor to advance to 1 after space, got %d", m.cursor)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(selectModel)
	if !m.checked["b"] {
		t.Fatalf("expected 'b' checked after space")
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if len(m.confirmed) != 2 {
		t.Fatalf("confirmed = %v, want 2 items", m.confirmed)
	}
	got := map[string]bool{m.confirmed[0]: true, m.confirmed[1]: true}
	if !got["a"] || !got["b"] {
		t.Fatalf("confirmed = %v, want [a b]", m.confirmed)
	}
}

func TestSelectModelSpaceAdvancesButDoesNotOvershoot(t *testing.T) {
	m := newSelectModel("", []string{"a", "b"}, true, nil, true)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(selectModel)
	if m.cursor != 1 {
		t.Fatalf("after first space: cursor = %d, want 1", m.cursor)
	}

	// Toggling the last row must not move the cursor past it.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(selectModel)
	if m.cursor != 1 {
		t.Fatalf("after space on last row: cursor = %d, want 1 (no overshoot)", m.cursor)
	}
	if !m.checked["a"] || !m.checked["b"] {
		t.Fatalf("expected both items checked, got %v", m.checked)
	}
}

func TestSelectModelCursorFallbackDisabledConfirmsEmpty(t *testing.T) {
	// Regression: a picker built with cursorFallback=false (selectMultiOptional)
	// must let a bare enter with nothing checked confirm an empty selection
	// instead of silently picking whatever row the cursor happens to be on --
	// this is what makes "select nothing" possible on `edit`'s add/remove
	// pickers.
	m := newSelectModel("", []string{"a", "b", "c"}, true, nil, false)
	m2, _ := m.Update(key('j'))
	m = m2.(selectModel)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if len(m.confirmed) != 0 {
		t.Fatalf("confirmed = %v, want empty (cursorFallback disabled)", m.confirmed)
	}
}

func TestSelectModelMultiEnterWithNoneCheckedUsesCursor(t *testing.T) {
	m := newSelectModel("", []string{"a", "b", "c"}, true, nil, true)
	m2, _ := m.Update(key('j'))
	m = m2.(selectModel)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if len(m.confirmed) != 1 || m.confirmed[0] != "b" {
		t.Fatalf("confirmed = %v, want [b]", m.confirmed)
	}
}

func TestSelectModelPreselected(t *testing.T) {
	m := newSelectModel("", []string{"a", "b", "c"}, true, map[string]bool{"a": true, "c": true}, false)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if len(m.confirmed) != 2 {
		t.Fatalf("confirmed = %v, want 2 preselected items", m.confirmed)
	}
}

func TestSelectModelPreselectedUncheckAllConfirmsEmpty(t *testing.T) {
	// Regression: a picker that starts fully preselected (patches: "select
	// all by default, uncheck the ones you don't want") must let the user
	// uncheck everything and get an empty result — it must NOT fall back
	// to selecting whatever's under the cursor, since that fallback exists
	// only for pickers that started with nothing checked.
	m := newSelectModel("", []string{"a", "b"}, true, map[string]bool{"a": true, "b": true}, false)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace}) // uncheck "a" (cursor starts at 0)
	m = m2.(selectModel)
	m2, _ = m.Update(key('j'))
	m = m2.(selectModel)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // uncheck "b"
	m = m2.(selectModel)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)

	if len(m.confirmed) != 0 {
		t.Fatalf("confirmed = %v, want empty (deliberate none-selected)", m.confirmed)
	}
}

func TestSelectModelFilter(t *testing.T) {
	m := newSelectModel("", []string{"apple", "banana", "cherry"}, false, nil, true)

	m2, _ := m.Update(key('/'))
	m = m2.(selectModel)
	if !m.filtering {
		t.Fatalf("expected filtering mode after '/'")
	}

	m2, _ = m.Update(key('b'))
	m = m2.(selectModel)
	m2, _ = m.Update(key('a'))
	m = m2.(selectModel)

	if len(m.filtered) != 1 || m.items[m.filtered[0]] != "banana" {
		t.Fatalf("filtered = %v, want just banana", m.filtered)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)
	if m.filtering {
		t.Fatalf("expected enter to leave filtering mode")
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)
	if len(m.confirmed) != 1 || m.confirmed[0] != "banana" {
		t.Fatalf("confirmed = %v, want [banana]", m.confirmed)
	}
}

func TestSelectModelEscClearsFilterBeforeCancelling(t *testing.T) {
	m := newSelectModel("", []string{"apple", "banana", "cherry"}, false, nil, true)

	m2, _ := m.Update(key('/'))
	m = m2.(selectModel)
	m2, _ = m.Update(key('b'))
	m = m2.(selectModel)
	if len(m.filtered) != 1 {
		t.Fatalf("expected filter to narrow to 1 item, got %v", m.filtered)
	}

	// Esc while typing the filter: clears it and exits filter-edit mode,
	// but must NOT quit the picker.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(selectModel)
	if m.filtering {
		t.Fatalf("expected esc to leave filter-edit mode")
	}
	if m.filter != "" {
		t.Fatalf("expected esc to clear the filter, got %q", m.filter)
	}
	if m.done || cmd != nil {
		t.Fatalf("expected esc-to-clear not to quit the picker, got done=%v cmd=%v", m.done, cmd)
	}

	// A second Esc, with no filter left, cancels the picker.
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(selectModel)
	if !m.done || !m.cancelled || cmd == nil {
		t.Fatalf("expected second esc to cancel, got done=%v cancelled=%v cmd=%v", m.done, m.cancelled, cmd)
	}
}

func TestSelectModelEscInNormalModeClearsLingeringFilter(t *testing.T) {
	m := newSelectModel("", []string{"apple", "banana", "cherry"}, false, nil, true)

	m2, _ := m.Update(key('/'))
	m = m2.(selectModel)
	m2, _ = m.Update(key('b'))
	m = m2.(selectModel)
	// Confirm the filter with enter (stays applied, back in normal mode)
	// instead of clearing it with esc.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(selectModel)
	if m.filtering || m.filter != "b" {
		t.Fatalf("expected filter 'b' to remain active in normal mode, got filtering=%v filter=%q", m.filtering, m.filter)
	}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(selectModel)
	if m.filter != "" {
		t.Fatalf("expected esc in normal mode to clear the lingering filter, got %q", m.filter)
	}
	if m.done || cmd != nil {
		t.Fatalf("expected esc-to-clear not to quit the picker, got done=%v cmd=%v", m.done, cmd)
	}
}

func TestSelectModelCancel(t *testing.T) {
	m := newSelectModel("", []string{"a", "b"}, false, nil, true)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(selectModel)

	if !m.done || !m.cancelled {
		t.Fatalf("expected done && cancelled after esc, got done=%v cancelled=%v", m.done, m.cancelled)
	}
	// Regression: Update must return tea.Quit once done, or bubbletea's
	// event loop never exits and the program hangs with a blank screen
	// (View() renders "" once done) instead of returning control.
	if cmd == nil {
		t.Fatalf("expected Update to return tea.Quit once done, got nil cmd")
	}
}

func TestSelectModelUpdateQuitsOnceDone(t *testing.T) {
	cases := []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
	}
	for _, key := range cases {
		m := newSelectModel("", []string{"a", "b"}, false, nil, true)
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %v: expected a non-nil cmd (tea.Quit) once the model is done", key)
		}
	}

	// Keys that don't finish the picker must not quit.
	live := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'j'}},
		{Type: tea.KeyRunes, Runes: []rune{'/'}},
	}
	for _, key := range live {
		m := newSelectModel("", []string{"a", "b"}, false, nil, true)
		_, cmd := m.Update(key)
		if cmd != nil {
			t.Fatalf("key %v: expected nil cmd while picker is still active", key)
		}
	}
}
