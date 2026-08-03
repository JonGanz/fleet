package main

import (
	"fmt"
)

// jumpEntry is one selectable row for `fleet-task jump`.
type jumpEntry struct {
	Ticket       string
	Repo         string
	WorktreePath string
}

func (e jumpEntry) line() string {
	return fmt.Sprintf("%s\t%s\t%s", e.Ticket, e.Repo, e.WorktreePath)
}

// buildJumpEntries flattens all tasks' repos into a flat list of jump
// entries, for piping into fzf.
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

	lines := make([]string, 0, len(entries))
	byLine := make(map[string]jumpEntry, len(entries))
	for _, e := range entries {
		l := e.line()
		lines = append(lines, l)
		byLine[l] = e
	}

	chosen, err := fzfSelectOne(lines)
	if err != nil {
		// Cancelled (e.g. Esc) or fzf missing: exit non-zero, no stdout.
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
