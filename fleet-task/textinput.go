package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// promptText shows a single-line bubbletea text input for label, with
// left/right/home/end cursor movement and backspace/delete -- replacing the
// old bufio.Reader-based prompts, which had no line editing at all beyond
// whatever the terminal driver itself provided (no arrow-key support).
// Returns the trimmed input text, or an error if the user cancels
// (esc/ctrl-c).
func promptText(label string) (string, error) {
	m := newTextInputModel(label)

	// Same /dev/tty wiring as select.go's runSelect, for the same reason:
	// a controlling terminal is needed regardless of how the caller's own
	// stdin/stdout are redirected.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open /dev/tty (a controlling terminal is required for interactive input): %w", err)
	}
	defer tty.Close()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("prompt: %w", err)
	}
	final := result.(textInputModel)
	if final.cancelled {
		return "", fmt.Errorf("input cancelled")
	}
	return strings.TrimSpace(string(final.value)), nil
}

type textInputModel struct {
	label  string
	value  []rune
	cursor int

	cancelled bool
	done      bool
}

func newTextInputModel(label string) textInputModel {
	return textInputModel{label: label}
}

func (m textInputModel) Init() tea.Cmd {
	return nil
}

func (m textInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.cancelled = true
		m.done = true
	case tea.KeyEnter:
		m.done = true
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight:
		if m.cursor < len(m.value) {
			m.cursor++
		}
	case tea.KeyHome:
		m.cursor = 0
	case tea.KeyEnd:
		m.cursor = len(m.value)
	case tea.KeyBackspace:
		if m.cursor > 0 {
			m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
			m.cursor--
		}
	case tea.KeyDelete:
		if m.cursor < len(m.value) {
			m.value = append(m.value[:m.cursor], m.value[m.cursor+1:]...)
		}
	case tea.KeyCtrlU:
		m.value = m.value[m.cursor:]
		m.cursor = 0
	case tea.KeyRunes:
		rest := append([]rune{}, m.value[m.cursor:]...)
		m.value = append(append(m.value[:m.cursor:m.cursor], keyMsg.Runes...), rest...)
		m.cursor += len(keyMsg.Runes)
	}

	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m textInputModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.label + ": ")
	for i, r := range m.value {
		if i == m.cursor {
			b.WriteString(selectCursorStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	if m.cursor == len(m.value) {
		b.WriteString(selectCursorStyle.Render(" "))
	}
	b.WriteString("\n")
	b.WriteString(selectDimStyle.Render("enter confirm  esc/ctrl-c cancel"))
	return b.String()
}
