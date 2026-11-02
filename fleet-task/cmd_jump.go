package main

import "fmt"

// jumpEntry is one selectable row for `fleet-task jump`.
type jumpEntry struct {
	Ticket       string
	Repo         string
	WorktreePath string
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

// cmdJump prints a chosen worktree path. If ticket is non-empty, the picker
// is restricted to that ticket's repos.
func cmdJump(ticket string) error {
	td, err := tasksDir()
	if err != nil {
		return err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return err
	}

	if ticket != "" {
		if err := validateTicketName(ticket); err != nil {
			return err
		}
		var filtered []*TaskState
		for _, t := range tasks {
			if t.Ticket == ticket {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no task found for ticket %s", ticket)
		}
		tasks = filtered
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

	chosen, err := selectOneLine(lines)
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
