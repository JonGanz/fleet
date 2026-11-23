// Command fleet-task manages per-ticket git-worktree sets: creating a
// worktree per selected repo for a ticket, listing/jumping between
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
  new [ticket]          create a task's worktrees (interactive unless --repo is given)
  list [--json]         list existing tasks
  show <ticket>         show one task's repos, branches, worktrees and patches
  repos [--json]        list configured repos, their run targets and patches
  patches [repo]        list available patches with summaries and touched files
  jump [ticket]         print a worktree path (picker unless --repo or only one match)
  edit [ticket]         add/remove repos (interactive unless --add/--remove is given)
  mv [old] [new]        rename a task's ticket id (prompts for whatever is omitted)
  rm [ticket]           remove a task's worktrees and state file

Without a terminal on stdin (or with FLEET_NO_INPUT=1) no picker or prompt is
ever shown: a missing value is an error naming the flag that supplies it.
Exit codes: 0 ok, 1 operation failed, 2 usage error or input required.

Run `+"`fleet-task <command> --help`"+` for a command's flags.`)
}

var (
	specEdit    = cmdSpec{name: "edit", synopsis: "edit [ticket] [--add <repo>]... [--remove <repo>]... [--skip-patch <repo>:<name>]... [--no-patches] [--json]", summary: "Add and/or remove repos of an existing task. --add/--remove skip the pickers; added repos then get every available patch unless --skip-patch/--no-patches say otherwise.", maxPos: 1}
	specJump    = cmdSpec{name: "jump", synopsis: "jump [ticket] [--repo <name>]", summary: "Print a worktree path on stdout. Picks without a picker when --repo is given or only one worktree matches.", maxPos: 1}
	specList    = cmdSpec{name: "list", synopsis: "list [--json]", summary: "List existing tasks.", maxPos: 0}
	specMv      = cmdSpec{name: "mv", synopsis: "mv [old] [new] [--json]", summary: "Rename a task's ticket id, moving its worktrees. Rerunnable after a partial failure. Prompts for whatever is omitted.", maxPos: 2}
	specNew     = cmdSpec{name: "new", synopsis: "new [ticket] [-d <description>] [--repo <name>]... [--skip-patch <repo>:<name>]... [--no-patches] [--json]", summary: "Create a task: a worktree per repo. With a ticket and --repo nothing is prompted; every available patch is applied unless --skip-patch/--no-patches say otherwise. Without them, prompts for the ticket/description and picks repos and patches.", maxPos: 1}
	specPatches = cmdSpec{name: "patches", synopsis: "patches [repo] [--json]", summary: "List patches under <config dir>/patches/<repo>/ with a one-line summary and the files each touches.", maxPos: 1}
	specRepos   = cmdSpec{name: "repos", synopsis: "repos [--json]", summary: "List repos configured in repos.yaml with their runtime, default branch, fleet-run targets and available patches.", maxPos: 0}
	specRm      = cmdSpec{name: "rm", synopsis: "rm [ticket] [--json]", summary: "Remove a task's worktrees and state file. Shows a ticket picker if no ticket is given.", maxPos: 1}
	specShow    = cmdSpec{name: "show", synopsis: "show <ticket> [--json]", summary: "Show one task: description, repos, branches, worktree paths, applied patches, and whether fleet-run has it active.", maxPos: 1}
)

const (
	jsonHelp      = "print a machine-readable JSON result on stdout (progress goes to stderr)"
	noPatchesHelp = "apply no patches to the repos being added"
	skipPatchHelp = "`repo:name` of a patch not to apply (repeatable or comma-separated)"
)

// at returns args[i], or "" if absent.
func at(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func main() {
	err := run(os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	if isUsageError(err) {
		os.Exit(2)
	}
	os.Exit(1)
}

func run(args []string) error {
	if len(args) < 1 {
		usage()
		return usageErrorf("a command is required")
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "edit":
		return runEdit(rest)
	case "jump":
		return runJump(rest)
	case "list":
		jsonOut := false
		if _, err := parseCmdArgs(specList, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, "print the raw JSON array instead of a table")
		}); err != nil {
			return err
		}
		return cmdList(jsonOut)
	case "mv":
		jsonOut := false
		pos, err := parseCmdArgs(specMv, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		return cmdMv(at(pos, 0), at(pos, 1), jsonOut)
	case "new":
		return runNew(rest)
	case "patches":
		jsonOut := false
		pos, err := parseCmdArgs(specPatches, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		return cmdPatches(at(pos, 0), jsonOut)
	case "repos":
		jsonOut := false
		if _, err := parseCmdArgs(specRepos, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		}); err != nil {
			return err
		}
		return cmdRepos(jsonOut)
	case "rm":
		jsonOut := false
		pos, err := parseCmdArgs(specRm, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		return cmdRm(at(pos, 0), jsonOut)
	case "show":
		jsonOut := false
		pos, err := parseCmdArgs(specShow, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		if at(pos, 0) == "" {
			return usageErrorf("show needs a ticket: `fleet-task show <ticket>`")
		}
		return cmdShow(pos[0], jsonOut)
	case "-h", "--help", "help":
		usage()
		return flag.ErrHelp
	default:
		usage()
		return usageErrorf("unknown command %q", cmd)
	}
}

func runEdit(args []string) error {
	var o editOpts
	var add, remove, skip listFlag
	pos, err := parseCmdArgs(specEdit, args, func(fs *flag.FlagSet) {
		fs.Var(&add, "add", "`repo` to add (repeatable or comma-separated)")
		fs.Var(&remove, "remove", "`repo` to remove (repeatable or comma-separated)")
		fs.Var(&skip, "skip-patch", skipPatchHelp)
		fs.BoolVar(&o.NoPatches, "no-patches", false, noPatchesHelp)
		fs.BoolVar(&o.JSON, "json", false, jsonHelp)
	})
	if err != nil {
		return err
	}
	o.Ticket, o.Add, o.Remove, o.SkipPatches = at(pos, 0), add, remove, skip
	return cmdEdit(o)
}

func runJump(args []string) error {
	repo := ""
	pos, err := parseCmdArgs(specJump, args, func(fs *flag.FlagSet) {
		fs.StringVar(&repo, "repo", "", "`repo` whose worktree path to print (requires a ticket)")
	})
	if err != nil {
		return err
	}
	return cmdJump(at(pos, 0), repo)
}

func runNew(args []string) error {
	var o newOpts
	var repos, skip listFlag
	var description trackedString
	pos, err := parseCmdArgs(specNew, args, func(fs *flag.FlagSet) {
		fs.Var(&description, "description", "task `description` (used by branch templates)")
		fs.Var(&description, "d", "shorthand for --description")
		fs.Var(&repos, "repo", "`repo` to create a worktree for (repeatable or comma-separated)")
		fs.Var(&skip, "skip-patch", skipPatchHelp)
		fs.BoolVar(&o.NoPatches, "no-patches", false, noPatchesHelp)
		fs.BoolVar(&o.JSON, "json", false, jsonHelp)
	})
	if err != nil {
		return err
	}
	o.Ticket, o.Repos, o.SkipPatches = at(pos, 0), repos, skip
	o.Description, o.DescriptionSet = description.value, description.set
	return cmdNew(o)
}
