package main

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
)

// jumpEntry is one selectable row for `fleet-task jump`.
type jumpEntry struct {
	Ticket       string
	Repo         string
	WorktreePath string
}

// jumpEntryLines renders entries as column-aligned rows using tabwriter,
// which pads with real spaces rather than leaving literal tab characters
// in the string. Raw tabs expand to the terminal's tab stops, which
// depend on the cursor column reached by whatever text preceded them —
// different per row since ticket/repo names vary in length — so the
// picker's columns visibly drifted row to row and its line-diffing
// repaint left stale characters behind (rendering as duplicated text).
// Fixed-width rows sidestep both.
func jumpEntryLines(entries []jumpEntry) []string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Ticket, e.Repo, e.WorktreePath)
	}
	tw.Flush()
	out := strings.TrimSuffix(buf.String(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// buildJumpEntries flattens all tasks' repos into a flat list of jump
// entries, for feeding into selectOne.
func buildJumpEntries(tasks []*TaskState) []jumpEntry {
	var entries []jumpEntry
	for _, t := range tasks {
		for _, r := range t.Repos {
			entries = append(entries, jumpEntry{
				Ticket:       t.Ticket,
				Repo:         r.Repo,
				WorktreePath: r.WorktreePath,
			})
		}
	}
	return entries
}

func cmdJump() error {
	td, err := tasksDir()
	if err != nil {
		return err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return err
	}

	entries := buildJumpEntries(tasks)
	if len(entries) == 0 {
		return fmt.Errorf("no tasks found")
	}

	lines := jumpEntryLines(entries)
	byLine := make(map[string]jumpEntry, len(entries))
	for i, e := range entries {
		byLine[lines[i]] = e
	}

	chosen, err := selectOne(lines)
	if err != nil {
		// Cancelled (e.g. Esc/q): exit non-zero, no stdout.
		return err
	}

	entry, ok := byLine[chosen]
	if !ok {
		return fmt.Errorf("internal error: selection %q not found", chosen)
	}

	// Print ONLY the worktree path, per contract, so it composes with:
	//   fj() { local d; d=$(fleet-task jump) && cd "$d"; }
	fmt.Println(entry.WorktreePath)
	return nil
}
