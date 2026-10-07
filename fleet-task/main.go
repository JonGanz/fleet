// Command fleet-task manages per-ticket git-worktree sets: creating a
// worktree per selected repo for a Jira ticket, listing/jumping between
// existing tasks' worktrees, and tearing them down. See
// docs/CONTRACT.md in the fleet monorepo for the full shared contract.
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: fleet-task <command> [args]

commands:
  new              interactively create a new task's worktrees
  list [--json]    list existing tasks
  jump             select a worktree and print its path (for `+"`cd $(fleet-task jump)`"+`)
  edit [ticket]    add and/or remove repos from an existing task (interactive picker if omitted)
  mv [old] [new]   rename a task's ticket id (interactive picker/prompt if omitted)
  rm [ticket]      remove a task's worktrees and state file (interactive picker if omitted)`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew()
	case "list":
		jsonOut := false
		for _, a := range os.Args[2:] {
			if a == "--json" {
				jsonOut = true
			}
		}
		err = cmdList(jsonOut)
	case "jump":
		err = cmdJump()
	case "edit":
		ticket := ""
		if len(os.Args) >= 3 {
			ticket = os.Args[2]
		}
		err = cmdEdit(ticket)
	case "mv":
		oldTicket, newTicket := "", ""
		if len(os.Args) >= 3 {
			oldTicket = os.Args[2]
		}
		if len(os.Args) >= 4 {
			newTicket = os.Args[3]
		}
		err = cmdMv(oldTicket, newTicket)
	case "rm":
		ticket := ""
		if len(os.Args) >= 3 {
			ticket = os.Args[2]
		}
		err = cmdRm(ticket)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet-task: %v\n", err)
		os.Exit(1)
	}
}
