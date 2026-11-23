package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

// listEntry is one task in `list --json`. Without --status it encodes
// exactly like TaskState; with it, the counts are added.
type listEntry struct {
	*TaskState
	DirtyRepos   *int `json:"dirty_repos,omitempty"`
	UnknownRepos *int `json:"unknown_repos,omitempty"`
}

// cmdList prints every task. withStatus also checks each repo's worktree
// for uncommitted changes (inspectRepoStatus) -- opt-in, because that runs
// several git commands per worktree (powershell.exe round trips for
// windows repos), which is slow across many tasks.
func cmdList(jsonOut, withStatus bool) error {
	td, err := tasksDir()
	if err != nil {
		return err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return err
	}

	var cfg *ReposConfig
	if withStatus {
		cfg = loadReposConfigOptional()
	}
	entries := make([]listEntry, len(tasks))
	for i, t := range tasks {
		entries[i] = listEntry{TaskState: t}
		if withStatus {
			dirty, unknown := countRepoChanges(cfg, t)
			entries[i].DirtyRepos, entries[i].UnknownRepos = &dirty, &unknown
		}
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	header := "TICKET\tDESCRIPTION\tREPOS\tCREATED_AT"
	if withStatus {
		header += "\tCHANGES"
	}
	fmt.Fprintln(w, header)
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s", e.Ticket, e.Description, len(e.Repos), e.CreatedAt.Format("2006-01-02 15:04:05"))
		if withStatus {
			fmt.Fprintf(w, "\t%s", formatRepoChangeCounts(*e.DirtyRepos, *e.UnknownRepos))
		}
		fmt.Fprintln(w)
	}
	return w.Flush()
}

// countRepoChanges counts a task's repos whose worktree is dirty, and those
// that couldn't be checked.
func countRepoChanges(cfg *ReposConfig, t *TaskState) (dirty, unknown int) {
	for _, r := range t.Repos {
		switch inspectRepoStatus(cfg, r).Changes.State {
		case changesDirty:
			dirty++
		case changesUnknown:
			unknown++
		}
	}
	return dirty, unknown
}

// formatRepoChangeCounts renders list's CHANGES column, e.g. "clean",
// "2 dirty", "1 dirty, 1 unknown".
func formatRepoChangeCounts(dirty, unknown int) string {
	switch {
	case dirty == 0 && unknown == 0:
		return changesClean
	case unknown == 0:
		return fmt.Sprintf("%d dirty", dirty)
	case dirty == 0:
		return fmt.Sprintf("%d unknown", unknown)
	default:
		return fmt.Sprintf("%d dirty, %d unknown", dirty, unknown)
	}
}
