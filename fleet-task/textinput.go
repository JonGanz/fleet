package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// promptForm shows every field in labels at once, each in its own bordered
// box, and lets the user move between them in any order (tab/shift+tab or
// up/down) instead of answering one bufio.Reader prompt at a time. Within
// the focused field, left/right/home/end move the cursor and
// backspace/delete edit it. Enter on any field but the last advances focus
// to the next one; enter on the last field submits the whole form. Returns
// the entered values in the same order as labels, or an error if the user
// cancels (esc/ctrl-c).
func promptForm(labels []string) ([]string, error) {
	m := newFormModel(labels)

	// Same /dev/tty wiring as select.go's runSelect, for the same reason:
	// a controlling terminal is needed regardless of how the caller's own
	// stdin/stdout are redirected.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty (a controlling terminal is required for interactive input): %w", err)
	}
	defer tty.Close()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("prompt: %w", err)
	}
	final := result.(formModel)
	if final.cancelled {
		return nil, fmt.Errorf("input cancelled")
	}

	values := make([]string, len(final.fields))
	for i, f := range final.fields {
		values[i] = strings.TrimSpace(string(f.value))
	}
	return values, nil
}

// promptText is promptForm for a single field, for callers that only need
// one value.
func promptText(label string) (string, error) {
	values, err := promptForm([]string{label})
	if err != nil {
		return "", err
	}
	return values[0], nil
}

type formField struct {
	label  string
	value  []rune
	cursor int
}

type formModel struct {
	fields []formField
	focus  int

	cancelled bool
	done      bool
}

func newFormModel(labels []string) formModel {
	fields := make([]formField, len(labels))
	for i, l := range labels {
		fields[i] = formField{label: l}
	}
	return formModel{fields: fields}
}

func (m formModel) Init() tea.Cmd {
	return nil
}

func (m formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.cancelled = true
		m.done = true
	case tea.KeyTab, tea.KeyDown:
		m.focus = (m.focus + 1) % len(m.fields)
	case tea.KeyShiftTab, tea.KeyUp:
		m.focus = (m.focus - 1 + len(m.fields)) % len(m.fields)
	case tea.KeyEnter:
		if m.focus == len(m.fields)-1 {
			m.done = true
		} else {
			m.focus++
		}
	default:
		m.fields[m.focus] = updateField(m.fields[m.focus], keyMsg)
	}

	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

// updateField applies a single-field edit key to f. Space arrives as its
// own tea.KeySpace type rather than folded into KeyRunes, so it needs its
// own case -- without it, spaces were silently dropped instead of inserted.
func updateField(f formField, keyMsg tea.KeyMsg) formField {
	switch keyMsg.Type {
	case tea.KeyLeft:
		if f.cursor > 0 {
			f.cursor--
		}
	case tea.KeyRight:
		if f.cursor < len(f.value) {
			f.cursor++
		}
	case tea.KeyHome:
		f.cursor = 0
	case tea.KeyEnd:
		f.cursor = len(f.value)
	case tea.KeyBackspace:
		if f.cursor > 0 {
			f.value = append(f.value[:f.cursor-1], f.value[f.cursor:]...)
			f.cursor--
		}
	case tea.KeyDelete:
		if f.cursor < len(f.value) {
			f.value = append(f.value[:f.cursor], f.value[f.cursor+1:]...)
		}
	case tea.KeyCtrlU:
		f.value = f.value[f.cursor:]
		f.cursor = 0
	case tea.KeySpace:
		f.value = insertRunes(f.value, f.cursor, []rune{' '})
		f.cursor++
	case tea.KeyRunes:
		f.value = insertRunes(f.value, f.cursor, keyMsg.Runes)
		f.cursor += len(keyMsg.Runes)
	}
	return f
}

func insertRunes(value []rune, at int, runes []rune) []rune {
	rest := append([]rune{}, value[at:]...)
	return append(append(value[:at:at], runes...), rest...)
}

var (
	formFocusedBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("212")).
				Padding(0, 1)
	formBlurredBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(0, 1)
)

func (m formModel) View() string {
	if m.done {
		return ""
	}

	boxes := make([]string, 0, len(m.fields)+1)
	for i, f := range m.fields {
		style := formBlurredBorder
		if i == m.focus {
			style = formFocusedBorder
		}
		boxes = append(boxes, style.Render(f.label+"\n"+renderFieldValue(f, i == m.focus)))
	}

	help := "tab/shift+tab or up/down move field  enter next/submit  esc/ctrl-c cancel"
	boxes = append(boxes, selectDimStyle.Render(help))
	return lipgloss.JoinVertical(lipgloss.Left, boxes...)
}

func renderFieldValue(f formField, focused bool) string {
	if !focused {
		if len(f.value) == 0 {
			return " "
		}
		return string(f.value)
	}

	var b strings.Builder
	for i, r := range f.value {
		if i == f.cursor {
			b.WriteString(selectCursorStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	if f.cursor == len(f.value) {
		b.WriteString(selectCursorStyle.Render(" "))
	}
	return b.String()
}
