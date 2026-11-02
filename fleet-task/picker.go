package main

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
)

// alignColumns renders rows as column-aligned lines using tabwriter, which
// pads with real spaces rather than leaving literal tab characters in the
// string. Raw tabs expand to the terminal's tab stops, which depend on the
// cursor column reached by whatever text preceded them -- different per row
// since ticket/repo names vary in length -- so the picker's columns visibly
// drifted row to row and its line-diffing repaint left stale characters
// behind (rendering as duplicated text). Fixed-width rows sidestep both.
func alignColumns(rows [][]string) []string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
	out := strings.TrimSuffix(buf.String(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// rmEntryLines renders tasks as column-aligned "TICKET  DESCRIPTION" rows
// for the interactive ticket picker (see pickTicket).
func rmEntryLines(tasks []*TaskState) []string {
	rows := make([][]string, len(tasks))
	for i, t := range tasks {
		rows[i] = []string{t.Ticket, t.Description}
	}
	return alignColumns(rows)
}

// jumpEntryLines renders jump entries as column-aligned
// "TICKET  REPO  PATH" rows.
func jumpEntryLines(entries []jumpEntry) []string {
	rows := make([][]string, len(entries))
	for i, e := range entries {
		rows[i] = []string{e.Ticket, e.Repo, e.WorktreePath}
	}
	return alignColumns(rows)
}

// selectOneLine shows a single-select picker over lines and returns the
// chosen one.
func selectOneLine(lines []string) (string, error) {
	selected, err := runSelect(selectOpts{Items: lines, CursorFallback: true})
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("no selection made")
	}
	return selected[0], nil
}

// pickTicket shows an interactive picker over every existing task, for
// `rm`/`mv`/`edit` invoked with no ticket argument, and returns the chosen
// ticket id.
func pickTicket() (string, error) {
	td, err := tasksDir()
	if err != nil {
		return "", err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks found")
	}

	lines := rmEntryLines(tasks)
	byLine := make(map[string]string, len(tasks))
	for i, t := range tasks {
		byLine[lines[i]] = t.Ticket
	}

	chosen, err := selectOneLine(lines)
	if err != nil {
		return "", err
	}
	ticket, ok := byLine[chosen]
	if !ok {
		return "", fmt.Errorf("internal error: selection %q not found", chosen)
	}
	return ticket, nil
}
