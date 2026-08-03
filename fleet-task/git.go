package main

import (
	"fmt"
	"os"
	"os/exec"
)

// runGit runs `git <args...>` with stdout/stderr inherited, returning an
// error including combined context on failure.
func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}

// gitCloneBare runs `git clone --bare <origin> <base>`, then configures a
// standard remote-tracking fetch refspec and does an initial fetch.
//
// `git clone --bare` on its own copies the remote's branches straight into
// this repo's own refs/heads/* and does NOT set remote.origin.fetch, so
// refs like origin/<branch> never exist and a later `git fetch origin`
// would only update remote.origin.url bookkeeping, not any usable ref.
// Configuring the usual `+refs/heads/*:refs/remotes/origin/*` refspec
// up front (mirroring what a non-bare `git clone` does automatically)
// keeps `origin/<default_branch>` resolvable for worktree add, both now
// and after every subsequent gitFetch.
func gitCloneBare(origin, base string) error {
	if err := runGit("clone", "--bare", origin, base); err != nil {
		return err
	}
	if err := runGit("-C", base, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	return gitFetch(base)
}

// gitFetch runs `git -C <base> fetch origin`.
func gitFetch(base string) error {
	return runGit("-C", base, "fetch", "origin")
}

// gitWorktreeAddNewBranch runs
// `git -C <base> worktree add <worktreePath> -b <ticket> origin/<defaultBranch>`.
func gitWorktreeAddNewBranch(base, worktreePath, ticket, defaultBranch string) error {
	return runGit("-C", base, "worktree", "add", worktreePath, "-b", ticket, "origin/"+defaultBranch)
}

// gitWorktreeAddExistingBranch runs
// `git -C <base> worktree add <worktreePath> <ticket>` (branch already
// exists, e.g. a rerun of `fleet-task new` for the same ticket).
func gitWorktreeAddExistingBranch(base, worktreePath, ticket string) error {
	return runGit("-C", base, "worktree", "add", worktreePath, ticket)
}

// gitApplyPatch runs `git -C <worktreePath> apply <patchFile>`.
//
// We use `apply` rather than `am` for v1: `am` expects RFC 2822-style
// commit-message headers (as produced by `git format-patch`) and will
// fail/hang on a plain `git diff` output, whereas `apply` works uniformly
// for both patch styles as long as the patch doesn't need to *create* a
// commit. Since applied patches here are just working-tree changes ahead
// of the user's own commits in the new worktree, `apply` is simpler and
// sufficient; not preserving patch authorship/message metadata is an
// accepted trade-off for v1.
func gitApplyPatch(worktreePath, patchFile string) error {
	return runGit("-C", worktreePath, "apply", patchFile)
}

// gitWorktreeRemove runs `git -C <base> worktree remove <worktreePath> --force`.
func gitWorktreeRemove(base, worktreePath string) error {
	return runGit("-C", base, "worktree", "remove", worktreePath, "--force")
}

// checkSSHAgent runs `ssh-add -l` and prints a warning to stderr if it
// exits non-zero (no identities loaded / agent unreachable). This is
// advisory only -- some setups use agent forwarding this check can't see,
// so we never block on the result.
func checkSSHAgent() {
	cmd := exec.Command("ssh-add", "-l")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ssh-add -l failed (%v); no SSH identities may be loaded:\n%s\n", err, out)
	}
}
