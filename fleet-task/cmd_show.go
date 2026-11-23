package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// showResult is `fleet-task show --json`'s document: the task state plus
// whether fleet-run currently has it running.
type showResult struct {
	*TaskState
	Active bool `json:"active"`
}

// cmdShow prints one task's description, repos, branches, worktree paths and
// applied patches.
func cmdShow(ticket string, jsonOut bool) error {
	tf, err := taskFile(ticket)
	if err != nil {
		return usageErrorf("%v", err)
	}
	if _, err := os.Stat(tf); os.IsNotExist(err) {
		return fmt.Errorf("no task found for ticket %s (see `fleet-task list`)", ticket)
	}
	st, err := readTaskState(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}
	active, found := activeFleetTicket()
	res := showResult{TaskState: st, Active: found && active == st.Ticket}

	if jsonOut {
		return printJSON(res)
	}

	fmt.Printf("ticket:      %s\n", st.Ticket)
	fmt.Printf("description: %s\n", st.Description)
	fmt.Printf("created_at:  %s\n", st.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("active:      %t\n\n", res.Active)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tBRANCH\tPATCHES\tWORKTREE")
	for _, r := range st.Repos {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Repo, r.Branch, formatPatchList(r.Patches), r.WorktreePath)
	}
	return w.Flush()
}

// formatPatchList renders a TaskRepo's patches for the text table: "?" when
// unknown (task predates patch tracking), "-" when none were applied.
func formatPatchList(patches []string) string {
	switch {
	case patches == nil:
		return "?"
	case len(patches) == 0:
		return "-"
	default:
		return strings.Join(patches, ",")
	}
}
