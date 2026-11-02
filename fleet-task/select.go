package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// selectOpts configures runSelect, the single entry point for every
// in-process picker (replacing the old family of selectMulti*/selectOne
// wrappers). The picker has vim movement (j/k, gg/G), space to toggle a
// checkbox (multi mode), "/" to filter and enter to confirm.
type selectOpts struct {
	// Title is an optional line rendered above the picker, for pickers where
	// the item list alone doesn't say what's being chosen or why.
	Title string
	Items []string
	// Multi shows checkboxes and allows several items to be confirmed;
	// otherwise enter confirms the highlighted item.
	Multi bool
	// Preselected starts every item checked, so the user unchecks what they
	// don't want rather than checking what they do (e.g. patch selection).
	Preselected bool
	// CursorFallback makes a bare enter with nothing checked confirm the row
	// under the cursor (fzf's behavior). When false, it confirms an empty
	// selection -- for pickers where "select nothing" is a valid, deliberate
	// answer (e.g. `edit`'s add/remove repo pickers).
	CursorFallback bool
}

// runSelect runs the picker described by opts and returns the confirmed
// items. It returns (nil, nil) without showing anything when there are no
// items, and (nil, error) if the user cancels (esc/q/ctrl-c).
func runSelect(opts selectOpts) ([]string, error) {
	if len(opts.Items) == 0 {
		return nil, nil
	}
	var preselected map[string]bool
	if opts.Preselected {
		preselected = make(map[string]bool, len(opts.Items))
		for _, it := range opts.Items {
			preselected[it] = true
		}
	}
	m := newSelectModel(opts.Title, opts.Items, opts.Multi, preselected, opts.CursorFallback)

	// Render/read against the controlling terminal directly rather than
	// os.Stdin/os.Stdout: callers like `jump` are meant to be run as
	// `cd $(fleet-task jump)`, which redirects stdout into the command
	// substitution pipe. If the picker rendered there, the UI would be
	// invisible and its frames would leak into the captured selection.
	// Using /dev/tty for the UI (like fzf does) keeps stdout free for the
	// caller to print only the final chosen value.
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
	title    string
	items    []string
	filtered []int // indices into items matching the current filter
	checked  map[string]bool
	cursor   int
	multi    bool

	filtering bool
	filter    string

	pendingG bool // "g" pressed once, waiting for a second "g" (gg = go to top)

	// cursorFallback controls what a bare enter with nothing checked does:
	// true mirrors fzf's behavior of confirming whatever row the cursor is
	// on; false confirms an empty selection instead, for pickers where
	// "select nothing" is itself a valid, deliberate answer.
	cursorFallback bool

	confirmed []string
	cancelled bool
	done      bool
}

func newSelectModel(title string, items []string, multi bool, preselected map[string]bool, cursorFallback bool) selectModel {
	checked := make(map[string]bool, len(preselected))
	for k, v := range preselected {
		checked[k] = v
	}
	m := selectModel{
		title:          title,
		items:          items,
		checked:        checked,
		multi:          multi,
		cursorFallback: cursorFallback,
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
// checked yet and cursorFallback is enabled, the item currently under the
// cursor — mirroring fzf's behavior where a bare enter picks the
// highlighted line.
func (m selectModel) selection() []string {
	if m.multi {
		var out []string
		for _, item := range m.items {
			if m.checked[item] {
				out = append(out, item)
			}
		}
		if len(out) > 0 || !m.cursorFallback {
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

	if m.title != "" {
		b.WriteString(selectDimStyle.Render(m.title) + "\n")
	}

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
