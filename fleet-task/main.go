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
  jump             fzf-select a worktree and print its path (for `+"`cd $(fleet-task jump)`"+`)
  rm <ticket>      remove a task's worktrees and state file`)
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
	case "rm":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: fleet-task rm <ticket>")
			os.Exit(1)
		}
		err = cmdRm(os.Args[2])
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
