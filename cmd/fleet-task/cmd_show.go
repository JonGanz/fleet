package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// showResult is `fleet-task show --json`'s document: the task state plus
// whether fleet-run currently has it running. Its Repos shadows the
// embedded taskstate.State's (encoding/json prefers the shallower field), adding
// per-repo change information to the unchanged taskstate.Repo fields.
type showResult struct {
	*taskstate.State
	Repos  []showRepo `json:"repos"`
	Active bool       `json:"active"`
}

// showRepo is one repo in showResult: the state entry plus its uncommitted
// changes (beyond fleet-task's own patches) and unpushed commit count
// (null if unknown).
type showRepo struct {
	taskstate.Repo
	Changes         changeReport `json:"changes"`
	UnpushedCommits *int         `json:"unpushed_commits"`
}

// cmdShow prints one task's description, repos, branches, worktree paths and
// applied patches.
func cmdShow(ticket string, jsonOut bool) error {
	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	if err := requireTaskExists(ticket, tf); err != nil {
		return err
	}
	st, err := taskstate.Read(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}
	st.Ticket = ticket // the filename is authoritative (see taskstate.Read)
	active, found := activeFleetTicket()
	res := showResult{State: st, Repos: []showRepo{}, Active: found && active == st.Ticket}
	cfg := loadReposConfigOptional() // only needed to know each repo's runtime
	for _, r := range st.Repos {
		status := inspectRepoStatus(cfg, r)
		res.Repos = append(res.Repos, showRepo{Repo: r, Changes: status.Changes, UnpushedCommits: status.Unpushed})
	}

	if jsonOut {
		return cli.PrintJSON(res)
	}

	fmt.Printf("ticket:      %s\n", st.Ticket)
	fmt.Printf("description: %s\n", st.Description)
	fmt.Printf("created_at:  %s\n", st.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("active:      %t\n\n", res.Active)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tBRANCH\tPATCHES\tCHANGES\tUNPUSHED\tWORKTREE")
	for _, r := range res.Repos {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Repo.Repo, r.Branch, formatPatchList(r.Patches), formatChanges(r.Changes), formatUnpushed(r.UnpushedCommits), r.WorktreePath)
	}
	return w.Flush()
}

// formatPatchList renders a taskstate.Repo's patches for the text table: "?" when
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
