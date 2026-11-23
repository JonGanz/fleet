package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/taskstate"
)

// resolveTicketState figures out which ticket's task state to operate on.
//
// If ticket is non-empty, it validates it (taskstate.ValidateTicket, the
// same rule fleet-task applies), loads that ticket's state file directly and
// errors if it doesn't exist. Either way the loaded state's Ticket comes from
// the filename (see taskstate.Read). If ticket is empty, per fleet-run's contract
// there is no "plain checkout" path outside of worktrees to fall back to, so
// it requires there to be exactly one task file in tasksDir: zero or more
// than one is an error asking the caller to pass --ticket explicitly.
func resolveTicketState(tasksDirPath, ticket string) (*taskstate.State, error) {
	if ticket != "" {
		path, err := taskstate.Path(tasksDirPath, ticket)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(path); err != nil {
			// An unknown ticket is a usage error (exit 2) per the contract;
			// any other stat failure is a real one.
			if errors.Is(err, fs.ErrNotExist) {
				return nil, cli.Usagef("no task state found for ticket %q (expected %s; see `fleet-task list`)", ticket, path)
			}
			return nil, fmt.Errorf("no task state found for ticket %q (expected %s): %w", ticket, path, err)
		}
		return taskstate.Read(path)
	}

	files, err := taskstate.ListFiles(tasksDirPath)
	if err != nil {
		return nil, fmt.Errorf("list task files in %s: %w", tasksDirPath, err)
	}
	switch len(files) {
	case 0:
		return nil, fmt.Errorf("no tasks found in %s; pass --ticket to operate on a specific one (none exist yet, create one with fleet-task new)", tasksDirPath)
	case 1:
		return taskstate.Read(files[0])
	default:
		return nil, fmt.Errorf("multiple tasks exist (%s); pass --ticket to pick one", strings.Join(ticketsFromFiles(files), ", "))
	}
}

// ticketsFromFiles derives ticket ids from task file paths
// (<tasks dir>/<ticket>.json).
func ticketsFromFiles(files []string) []string {
	tickets := make([]string, 0, len(files))
	for _, f := range files {
		tickets = append(tickets, taskstate.TicketFromPath(f))
	}
	return tickets
}
