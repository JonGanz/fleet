package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// selectMulti replaces the old fzf --multi picker with an in-process
// bubbletea list: vim movement (j/k, gg/G), space to toggle a checkbox,
// "/" to filter, enter to confirm. Pressing enter with nothing checked
// confirms just the item under the cursor (mirrors fzf's behavior of a
// bare enter selecting the highlighted line). Returns (nil, error) if the
// user cancels (esc/ctrl-c), matching the old fzfSelectMulti contract.
func selectMulti(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return runSelect(items, true, nil)
}

// selectMultiPreselected is selectMulti but with every item checked by
// default, so the user can uncheck the ones they don't want instead of
// having to check everything they do. Unlike selectMulti, confirming
// with nothing checked returns an empty (not cursor-fallback) selection:
// once a picker starts preselected, an empty result is a deliberate
// "none of these" rather than "I forgot to check anything."
func selectMultiPreselected(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	checked := make(map[string]bool, len(items))
	for _, it := range items {
		checked[it] = true
	}
	return runSelect(items, true, checked)
}

// selectOne replaces the old fzf single-select picker with the same
// vim-navigable, filterable list, but without checkboxes: enter
// immediately confirms the highlighted item.
func selectOne(items []string) (string, error) {
	selected, err := runSelect(items, false, nil)
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("no selection made")
	}
	return selected[0], nil
}

func runSelect(items []string, multi bool, preselected map[string]bool) ([]string, error) {
	m := newSelectModel(items, multi, preselected)

	// Render/read against the controlling terminal directly rather than
	// os.Stdin/os.Stdout, mirroring fleet-task's picker and matching how
	// fzf (what this replaced) opened /dev/tty itself for keyboard/screen
	// handling regardless of how the caller's own stdin/stdout are wired.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty (a controlling terminal is required for interactive selection): %w", err)
	}
	defer tty.Close()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	final := result.(selectModel)
	if final.cancelled {
		return nil, fmt.Errorf("selection cancelled")
	}
	return final.confirmed, nil
}

var (
	selectCursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	selectCheckedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
	selectDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type selectModel struct {
	items    []string
	filtered []int // indices into items matching the current filter
	checked  map[string]bool
	cursor   int
	multi    bool

	filtering bool
	filter    string

	pendingG bool // "g" pressed once, waiting for a second "g" (gg = go to top)

	hasPreselection bool // true if the picker started with any item checked

	confirmed []string
	cancelled bool
	done      bool
}

func newSelectModel(items []string, multi bool, preselected map[string]bool) selectModel {
	checked := make(map[string]bool, len(preselected))
	hasPreselection := false
	for k, v := range preselected {
		checked[k] = v
		if v {
			hasPreselection = true
		}
	}
	m := selectModel{
		items:           items,
		checked:         checked,
		multi:           multi,
		hasPreselection: hasPreselection,
	}
	m.applyFilter()
	return m
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m *selectModel) applyFilter() {
	m.filtered = m.filtered[:0]
	for i, item := range m.items {
		if m.filter == "" || strings.Contains(strings.ToLower(item), strings.ToLower(m.filter)) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	var updated selectModel
	if m.filtering {
		updated = m.updateFiltering(keyMsg)
	} else {
		updated = m.updateNormal(keyMsg)
	}
	if updated.done {
		return updated, tea.Quit
	}
	return updated, nil
}

func (m selectModel) updateFiltering(msg tea.KeyMsg) selectModel {
	switch msg.Type {
	case tea.KeyEsc:
		// Esc while typing a filter clears it and drops back to normal
		// mode in one step (matches the "esc to clear" hint shown once a
		// filter is active) rather than leaving stale filter text behind
		// for a second, different-meaning Esc to cancel the whole picker.
		m.filtering = false
		m.filter = ""
		m.applyFilter()
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyBackspace:
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}
	case tea.KeyCtrlU:
		m.filter = ""
		m.applyFilter()
	case tea.KeyRunes:
		m.filter += string(msg.Runes)
		m.applyFilter()
	}
	return m
}

func (m selectModel) updateNormal(msg tea.KeyMsg) selectModel {
	wasPendingG := m.pendingG
	m.pendingG = false

	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		m.done = true
	case "esc":
		// Esc first clears a lingering filter (matches the "esc to clear"
		// hint shown while one is set); only cancels the picker once
		// there's no filter left to clear.
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		} else {
			m.cancelled = true
			m.done = true
		}
	case "q":
		m.cancelled = true
		m.done = true
	case "enter":
		m.confirmed = m.selection()
		m.done = true
	case "j", "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		if wasPendingG {
			m.cursor = 0
		} else {
			m.pendingG = true
		}
	case "G":
		m.cursor = len(m.filtered) - 1
	case " ":
		if m.multi && len(m.filtered) > 0 {
			item := m.items[m.filtered[m.cursor]]
			m.checked[item] = !m.checked[item]
			// Advance to the next row, same as j/down, so checking off a
			// run of items doesn't require a separate keypress between each
			// toggle. Doesn't wrap past the last row.
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		}
	case "a":
		if m.multi {
			for _, idx := range m.filtered {
				m.checked[m.items[idx]] = true
			}
		}
	case "A":
		if m.multi {
			m.checked = make(map[string]bool)
		}
	case "/":
		m.filtering = true
	}
	return m
}

// selection returns the checked items (multi mode) or, if nothing is
// checked yet, the item currently under the cursor — mirroring fzf's
// behavior where a bare enter picks the highlighted line.
func (m selectModel) selection() []string {
	if m.multi {
		var out []string
		for _, item := range m.items {
			if m.checked[item] {
				out = append(out, item)
			}
		}
		// A bare enter with nothing checked falls back to the item under
		// the cursor (mirrors fzf's behavior) — but only for pickers that
		// started with nothing preselected. If the picker started
		// preselected and the user unchecked everything, that's a
		// deliberate "select none," not "I forgot to check anything."
		if len(out) > 0 || m.hasPreselection {
			return out
		}
	}
	if len(m.filtered) == 0 {
		return nil
	}
	return []string{m.items[m.filtered[m.cursor]]}
}

func (m selectModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder

	prompt := "> "
	if m.filtering {
		prompt = "/" + m.filter
	} else if m.filter != "" {
		prompt = "/" + m.filter + " (esc to clear, / to edit)"
	}
	b.WriteString(prompt + "\n")

	for i, idx := range m.filtered {
		item := m.items[idx]

		cursorMark := "  "
		if i == m.cursor {
			cursorMark = "> "
		}

		checkbox := ""
		if m.multi {
			if m.checked[item] {
				checkbox = selectCheckedStyle.Render("[x] ")
			} else {
				checkbox = "[ ] "
			}
		}

		line := cursorMark + checkbox + item
		if i == m.cursor {
			line = selectCursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	help := "j/k move  gg/G top/bottom  / filter  enter confirm  esc/q cancel"
	if m.multi {
		help = "space toggle  a select all  A clear  " + help
	}
	b.WriteString(selectDimStyle.Render(help))

	return b.String()
}
