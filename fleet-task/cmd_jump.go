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
// is restricted to that ticket's repos; if repo is also given, or only one
// worktree remains, the path is printed without a picker.
func cmdJump(ticket, repo string) error {
	if repo != "" && ticket == "" {
		return usageErrorf("--repo requires a ticket: `fleet-task jump <ticket> --repo <name>`")
	}
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
	if repo != "" {
		entries = filterJumpEntries(entries, repo)
		if len(entries) == 0 {
			return fmt.Errorf("task %s has no repo %s (see `fleet-task show %s`)", ticket, repo, ticket)
		}
	}
	if len(entries) == 0 {
		return fmt.Errorf("no tasks found")
	}
	if len(entries) == 1 {
		fmt.Println(entries[0].WorktreePath)
		return nil
	}
	if !interactiveAllowed() {
		return needInput("a worktree choice", "pass `fleet-task jump <ticket> --repo <name>`")
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
	//   cd "$(fleet-task jump)"
	fmt.Println(entry.WorktreePath)
	return nil
}

// filterJumpEntries keeps only entries for repo.
func filterJumpEntries(entries []jumpEntry, repo string) []jumpEntry {
	var kept []jumpEntry
	for _, e := range entries {
		if e.Repo == repo {
			kept = append(kept, e)
		}
	}
	return kept
}
