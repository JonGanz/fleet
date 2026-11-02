// Command fleet-task manages per-ticket git-worktree sets: creating a
// worktree per selected repo for a Jira ticket, listing/jumping between
// existing tasks' worktrees, and tearing them down. See
// docs/CONTRACT.md in the fleet monorepo for the full shared contract.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: fleet-task <command> [flags] [args]

commands:
  new                   interactively create a new task's worktrees
  list [--json]         list existing tasks
  jump [ticket]         select a worktree and print its path (for `+"`cd $(fleet-task jump)`"+`)
  edit [ticket]         add and/or remove repos from an existing task (interactive picker if omitted)
  mv [old] [new]        rename a task's ticket id (interactive picker/prompt if omitted)
  rm [ticket]           remove a task's worktrees and state file (interactive picker if omitted)

Run `+"`fleet-task <command> --help`"+` for a command's flags.`)
}

var (
	specNew  = cmdSpec{name: "new", synopsis: "new", summary: "Interactively create a new task: prompts for a ticket id and description, picks repos and patches, and creates a worktree per repo.", maxPos: 0}
	specList = cmdSpec{name: "list", synopsis: "list [--json]", summary: "List existing tasks.", maxPos: 0}
	specJump = cmdSpec{name: "jump", synopsis: "jump [ticket]", summary: "Pick a worktree (optionally only among one ticket's repos) and print its path on stdout.", maxPos: 1}
	specEdit = cmdSpec{name: "edit", synopsis: "edit [ticket]", summary: "Add and/or remove repos of an existing task. Shows a ticket picker if no ticket is given.", maxPos: 1}
	specMv   = cmdSpec{name: "mv", synopsis: "mv [old] [new]", summary: "Rename a task's ticket id, moving its worktrees. Rerunnable after a partial failure. Prompts for whatever is omitted.", maxPos: 2}
	specRm   = cmdSpec{name: "rm", synopsis: "rm [ticket]", summary: "Remove a task's worktrees and state file. Shows a ticket picker if no ticket is given.", maxPos: 1}
)

// at returns args[i], or "" if absent.
func at(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func run(args []string) error {
	if len(args) < 1 {
		usage()
		return errors.New("a command is required")
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "new":
		if _, err := parseCmdArgs(specNew, rest, nil); err != nil {
			return err
		}
		return cmdNew()
	case "list":
		jsonOut := false
		pos, err := parseCmdArgs(specList, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, "print the raw JSON array instead of a table")
		})
		if err != nil {
			return err
		}
		_ = pos
		return cmdList(jsonOut)
	case "jump":
		pos, err := parseCmdArgs(specJump, rest, nil)
		if err != nil {
			return err
		}
		return cmdJump(at(pos, 0))
	case "edit":
		pos, err := parseCmdArgs(specEdit, rest, nil)
		if err != nil {
			return err
		}
		return cmdEdit(at(pos, 0))
	case "mv":
		pos, err := parseCmdArgs(specMv, rest, nil)
		if err != nil {
			return err
		}
		return cmdMv(at(pos, 0), at(pos, 1))
	case "rm":
		pos, err := parseCmdArgs(specRm, rest, nil)
		if err != nil {
			return err
		}
		return cmdRm(at(pos, 0))
	case "-h", "--help", "help":
		usage()
		return flag.ErrHelp
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
