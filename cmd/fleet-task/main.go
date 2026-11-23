// Command fleet-task manages per-ticket git-worktree sets: creating a
// worktree per selected repo for a ticket, listing/jumping between
// existing tasks' worktrees, and tearing them down. See
// docs/CONTRACT.md in the fleet monorepo for the full shared contract.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JonGanz/fleet/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=<v>"
// (install.sh does); see cli.Version for the fallback.
var version = "dev"

// usage prints the top-level help to w: stdout when asked for (`--help`),
// stderr alongside a usage error.
func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: fleet-task <command> [flags] [args]

commands:
  new [ticket]          create a task's worktrees (interactive unless --repo is given)
  list [--status]       list existing tasks (--status: count repos with uncommitted changes)
  show <ticket>         show one task's repos, branches, worktrees and patches
  repos [--json]        list configured repos, their run targets and patches
  patches [repo]        list available patches with summaries and touched files
  jump [ticket]         print a worktree path (picker unless --repo or only one match)
  edit [ticket]         add/remove repos (interactive unless --add/--remove is given)
  mv [old] [new]        rename a task's ticket id (prompts for whatever is omitted)
  rm [ticket]           remove a task's worktrees, state file and pushed branches
  unlock <ticket>       remove a ticket's lock left by a fleet-task that is gone
  version               print the fleet-task version (also --version)

Without a terminal on stdin (or with FLEET_NO_INPUT=1) no picker or prompt is
ever shown: a missing value is an error naming the flag that supplies it.
Exit codes: 0 ok, 1 operation failed, 2 usage error or input required.

Run `+"`fleet-task <command> --help`"+` for a command's flags.`)
}

var (
	specEdit    = cmdSpec{name: "edit", synopsis: "edit [ticket] [--add <repo>]... [--remove <repo>]... [--skip-patch <repo>:<name>]... [--no-patches] [--force] [--keep-branch | --delete-branch] [--json]", summary: "Add and/or remove repos of an existing task. --add/--remove skip the pickers; added repos then get every available patch unless --skip-patch/--no-patches say otherwise. Removing a repo whose worktree has uncommitted changes (beyond fleet-task's own patches) is refused, or asked about on a terminal, unless --force. A removed repo's branch is handled as by rm. An added repo whose worktree an interrupted run left in place (same base, same branch) is adopted.", maxPos: 1}
	specJump    = cmdSpec{name: "jump", synopsis: "jump [ticket] [--repo <name>]", summary: "Print a worktree path on stdout. Picks without a picker when --repo is given or only one worktree matches.", maxPos: 1}
	specList    = cmdSpec{name: "list", synopsis: "list [--status] [--json]", summary: "List existing tasks. --status also counts each task's repos with uncommitted changes (slower: runs git in every worktree).", maxPos: 0}
	specMv      = cmdSpec{name: "mv", synopsis: "mv [old] [new] [--json]", summary: "Rename a task's ticket id, moving its worktrees. Rerunnable after a partial failure. Prompts for whatever is omitted.", maxPos: 2}
	specNew     = cmdSpec{name: "new", synopsis: "new [ticket] [-d <description>] [--repo <name>]... [--skip-patch <repo>:<name>]... [--no-patches] [--json]", summary: "Create a task: a worktree per repo. With a ticket and --repo nothing is prompted; every available patch is applied unless --skip-patch/--no-patches say otherwise. Without them, prompts for the ticket/description and picks repos and patches.", maxPos: 1}
	specPatches = cmdSpec{name: "patches", synopsis: "patches [repo] [--json]", summary: "List patches under <config dir>/patches/<repo>/ with a one-line summary and the files each touches.", maxPos: 1}
	specRepos   = cmdSpec{name: "repos", synopsis: "repos [--json]", summary: "List repos configured in repos.yaml with their runtime, default branch, fleet-run targets and available patches.", maxPos: 0}
	specRm      = cmdSpec{name: "rm", synopsis: "rm [ticket] [--force] [--keep-branch | --delete-branch] [--json]", summary: "Remove a task's worktrees and state file. Shows a ticket picker if no ticket is given. Refuses (or asks, on a terminal) if a worktree has uncommitted changes beyond fleet-task's own patches, unless --force. Afterwards each repo's local branch is deleted from the base only if every commit on it is on a remote, else kept (--keep-branch: always keep; --delete-branch: delete even unpushed ones, after a confirmation that --force skips).", maxPos: 1}
	specUnlock  = cmdSpec{name: "unlock", synopsis: "unlock <ticket> [--force] [--json]", summary: "Remove the ticket's lock file left by a fleet-task that was killed. Refuses while the recorded owner pid is still running (or a pid-less lock is under a minute old), unless --force. No lock is success.", maxPos: 1}
	specShow    = cmdSpec{name: "show", synopsis: "show <ticket> [--json]", summary: "Show one task: description, repos, branches, worktree paths, applied patches, and whether fleet-run has it active.", maxPos: 1}
)

const (
	deleteBranchHelp = "delete removed repos' local branches even if they have commits on no remote (asks first; --force skips asking)"
	keepBranchHelp   = "keep removed repos' local branches (default: delete those whose commits are all on a remote)"
	forceHelp        = "remove worktrees even if they have uncommitted changes (skips the check, and --delete-branch's confirmation)"
	jsonHelp         = "print a machine-readable JSON result on stdout (progress goes to stderr)"
	noPatchesHelp    = "apply no patches to the repos being added"
	skipPatchHelp    = "`repo:name` of a patch not to apply (repeatable or comma-separated)"
)

// at returns args[i], or "" if absent.
func at(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func main() {
	cli.SetProgram("fleet-task")
	os.Exit(cli.Report(run(os.Args[1:])))
}

func run(args []string) error {
	if len(args) < 1 {
		usage(os.Stderr)
		return cli.Usagef("a command is required")
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "edit":
		return runEdit(rest)
	case "jump":
		return runJump(rest)
	case "list":
		jsonOut, withStatus := false, false
		if _, err := parseCmdArgs(specList, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, "print the raw JSON array instead of a table")
			fs.BoolVar(&withStatus, "status", false, "also count each task's repos with uncommitted changes (runs git in every worktree)")
		}); err != nil {
			return err
		}
		return cmdList(jsonOut, withStatus)
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
		jsonOut, force, keepBranch, deleteBranch := false, false, false, false
		pos, err := parseCmdArgs(specRm, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&force, "force", false, forceHelp)
			fs.BoolVar(&keepBranch, "keep-branch", false, keepBranchHelp)
			fs.BoolVar(&deleteBranch, "delete-branch", false, deleteBranchHelp)
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		mode, err := branchModeFromFlags(keepBranch, deleteBranch)
		if err != nil {
			return err
		}
		return cmdRm(at(pos, 0), force, mode, jsonOut)
	case "show":
		jsonOut := false
		pos, err := parseCmdArgs(specShow, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		if at(pos, 0) == "" {
			return cli.Usagef("show needs a ticket: `fleet-task show <ticket>`")
		}
		return cmdShow(pos[0], jsonOut)
	case "unlock":
		jsonOut, force := false, false
		pos, err := parseCmdArgs(specUnlock, rest, func(fs *flag.FlagSet) {
			fs.BoolVar(&force, "force", false, "remove the lock even though its owner pid is running (only when that pid is not a fleet-task)")
			fs.BoolVar(&jsonOut, "json", false, jsonHelp)
		})
		if err != nil {
			return err
		}
		return cmdUnlock(at(pos, 0), force, jsonOut)
	case "-h", "--help", "help":
		usage(os.Stdout)
		return flag.ErrHelp
	case "version", "--version":
		fmt.Printf("fleet-task %s\n", cli.Version(version))
		return nil
	default:
		usage(os.Stderr)
		return cli.Usagef("unknown command %q", cmd)
	}
}

// branchModeFromFlags turns rm/edit's --keep-branch/--delete-branch into a
// branch mode; both together is a usage error.
func branchModeFromFlags(keep, del bool) (string, error) {
	switch {
	case keep && del:
		return "", cli.Usagef("--keep-branch and --delete-branch are mutually exclusive")
	case keep:
		return branchKeep, nil
	case del:
		return branchDelete, nil
	}
	return branchAuto, nil
}

func runEdit(args []string) error {
	var o editOpts
	var add, remove, skip listFlag
	keepBranch, deleteBranch := false, false
	pos, err := parseCmdArgs(specEdit, args, func(fs *flag.FlagSet) {
		fs.Var(&add, "add", "`repo` to add (repeatable or comma-separated)")
		fs.Var(&remove, "remove", "`repo` to remove (repeatable or comma-separated)")
		fs.Var(&skip, "skip-patch", skipPatchHelp)
		fs.BoolVar(&o.NoPatches, "no-patches", false, noPatchesHelp)
		fs.BoolVar(&o.Force, "force", false, forceHelp)
		fs.BoolVar(&keepBranch, "keep-branch", false, keepBranchHelp)
		fs.BoolVar(&deleteBranch, "delete-branch", false, deleteBranchHelp)
		fs.BoolVar(&o.JSON, "json", false, jsonHelp)
	})
	if err != nil {
		return err
	}
	if o.BranchMode, err = branchModeFromFlags(keepBranch, deleteBranch); err != nil {
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
