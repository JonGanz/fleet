package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// rmEntryLines renders tasks as column-aligned "TICKET  DESCRIPTION" rows
// for `rm`'s interactive ticket picker, same tabwriter-padding approach as
// `jump`'s entries (see jumpEntryLines) so columns don't drift row to row.
func rmEntryLines(tasks []*TaskState) []string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	for _, t := range tasks {
		fmt.Fprintf(tw, "%s\t%s\n", t.Ticket, t.Description)
	}
	tw.Flush()
	out := strings.TrimSuffix(buf.String(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// pickTicket shows an interactive picker over every existing task, for
// `rm`/`mv`/`edit` invoked with no ticket argument, and returns the chosen ticket id.
func pickTicket() (string, error) {
	td, err := tasksDir()
	if err != nil {
		return "", err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks found")
	}

	lines := rmEntryLines(tasks)
	byLine := make(map[string]string, len(tasks))
	for i, t := range tasks {
		byLine[lines[i]] = t.Ticket
	}

	chosen, err := selectOne(lines)
	if err != nil {
		return "", err
	}
	ticket, ok := byLine[chosen]
	if !ok {
		return "", fmt.Errorf("internal error: selection %q not found", chosen)
	}
	return ticket, nil
}

func cmdRm(ticket string) error {
	if ticket == "" {
		t, err := pickTicket()
		if err != nil {
			return err
		}
		ticket = t
	}

	// Removing a worktree out from under a still-running fleet-run app is
	// what produces the "Directory not empty" mess selfHealWorktreeRemove
	// papers over above: a live dev server can be writing into its own
	// untracked cache dir at the exact moment git tries to delete it, and
	// the affected repo may have open file handles into its (hardlinked,
	// shared-cache) node_modules besides. Catch the common case up front
	// with an actionable error instead of racing it and hoping self-heal
	// covers whatever's left.
	if active, found := activeFleetTicket(); found && active == ticket {
		return fmt.Errorf("%s is currently active in fleet-run; run `fleet-run stop --ticket %s` (or `fleet-run stop --all`) before removing it", ticket, ticket)
	}

	tf, err := taskFile(ticket)
	if err != nil {
		return err
	}
	st, err := readTaskState(tf)
	if err != nil {
		return fmt.Errorf("read task %s: %w", ticket, err)
	}

	// repos.yaml is optional here: if it's missing or no longer has the
	// repo entry, we still try to remove the worktree (best-effort, via
	// git run from the worktree's parent) rather than aborting the whole
	// rm.
	var cfg *ReposConfig
	if rf, err := reposFile(); err == nil {
		if c, err := loadReposConfig(rf); err == nil {
			cfg = c
		}
	}

	var remaining []TaskRepo

	for _, r := range st.Repos {
		if rmErr := removeRepoWorktree(cfg, ticket, r); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: removing worktree %s: %v\n", r.WorktreePath, rmErr)
			remaining = append(remaining, r)
			continue
		}
		fmt.Printf("removed worktree %s (%s)\n", r.WorktreePath, r.Repo)
	}

	// If any worktree removal failed, keep the task state around (with only
	// the still-failing repos listed) instead of deleting it, so a rerun of
	// `rm` retries just those and the ticket doesn't silently vanish from
	// `fleet-task list`/`fleet-run` while its worktrees still exist on disk.
	if len(remaining) > 0 {
		st.Repos = remaining
		if err := writeTaskState(tf, st); err != nil {
			return fmt.Errorf("update task state %s after partial removal: %w", tf, err)
		}
		return fmt.Errorf("%d repo(s) failed to remove for %s; task state kept at %s for retry", len(remaining), ticket, tf)
	}

	if err := os.Remove(tf); err != nil {
		return fmt.Errorf("remove task state %s: %w", tf, err)
	}
	fmt.Printf("removed task state %s\n", tf)

	// Every repo's worktree_path is <worktree_root>/<ticket>/<repo> (per
	// docs/CONTRACT.md, true for runtime: windows repos too -- that path is
	// the WSL-side symlink even though the real tree lives under
	// windows_worktree_root), so once every repo is torn down the
	// per-ticket parent directory is left empty and should go with it
	// rather than accumulating stale empty dirs under worktree_root.
	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: resolving worktree root to remove ticket workspace dir: %v\n", err)
		return nil
	}
	ticketDir := filepath.Join(wtRoot, ticket)
	if err := os.RemoveAll(ticketDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: removing ticket workspace dir %s: %v\n", ticketDir, err)
		return nil
	}
	fmt.Printf("removed ticket workspace dir %s\n", ticketDir)
	return nil
}

// removeRepoWorktree tears down a single repo's worktree, shared between
// `rm` (removing every repo in a task) and `edit`'s remove-repo path
// (removing a selected subset). cfg may be nil, or missing this repo's
// entry entirely (repos.yaml changed, or the repo was dropped from it,
// since the task was created) -- best-effort falls back to a bare
// `git worktree remove` run against the worktree itself rather than
// aborting. pre-remove/post-remove hooks run around the teardown itself;
// like pre-create/post-create, hook failures are non-fatal (warned to
// stderr by runHooks) and never block the removal.
func removeRepoWorktree(cfg *ReposConfig, ticket string, r TaskRepo) error {
	if err := runHooks("pre-remove", ticket, r.Repo, r.WorktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: pre-remove hooks for %s: %v\n", r.Repo, err)
	}

	repo := cfg.findRepo(r.Repo)

	var removeErr error
	switch {
	case repo != nil && repo.Runtime == "windows":
		removeErr = removeWorktreeWindows(repo, r.WorktreePath)
	case repo != nil && expandHome(repo.Base) != "":
		removeErr = selfHealWorktreeRemove(r.WorktreePath, gitWorktreeRemove(expandHome(repo.Base), r.WorktreePath))
	default:
		fmt.Fprintf(os.Stderr, "warning: repo %q not found in repos.yaml, trying worktree remove without a base\n", r.Repo)
		removeErr = selfHealWorktreeRemove(r.WorktreePath, runGit("-C", r.WorktreePath, "worktree", "remove", r.WorktreePath, "--force"))
	}
	if removeErr != nil {
		return removeErr
	}

	if err := runHooks("post-remove", ticket, r.Repo, r.WorktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post-remove hooks for %s: %v\n", r.Repo, err)
	}
	return nil
}

// selfHealWorktreeRemove papers over a specific `git worktree remove --force`
// failure mode: git deregisters the worktree (removes its .git file/entry
// and everything it tracks) and only then hits a filesystem error trying to
// rmdir the now-empty-of-tracked-content directory, because something else
// -- typically a still-running dev server for this same ticket under
// `fleet-run`, writing into an untracked cache dir like `.vite/deps_temp_*`
// -- raced the delete and repopulated it, so git's own rmdir sees "Directory
// not empty". At that point git has fully committed to removing the
// worktree; there's nothing left to retry via git, so a plain rerun of `rm`
// just fails again with a *different*, more confusing error ("not a working
// tree") since git no longer considers the path a worktree at all. Detect
// that state (worktreePath still exists on disk but no longer has a .git
// entry inside it) and sweep the untracked leftovers ourselves instead of
// leaving the caller to retry something that can never succeed the same way
// twice.
func selfHealWorktreeRemove(worktreePath string, removeErr error) error {
	if removeErr == nil {
		return nil
	}
	if _, statErr := os.Stat(worktreePath); statErr != nil {
		// Nothing on disk to sweep -- surface the original error as-is.
		return removeErr
	}
	if !worktreeAdminGone(worktreePath) {
		// git still considers this a live worktree; the failure is real.
		return removeErr
	}
	if rmErr := os.RemoveAll(worktreePath); rmErr != nil {
		return fmt.Errorf("%w (also failed to remove leftover directory: %v)", removeErr, rmErr)
	}
	fmt.Fprintf(os.Stderr, "note: git had already deregistered %s; removed leftover directory\n", worktreePath)
	return nil
}

// worktreeAdminGone reports whether worktreePath no longer has a .git entry
// inside it, meaning git has already fully torn down its worktree
// registration and tracked content -- see selfHealWorktreeRemove.
func worktreeAdminGone(worktreePath string) bool {
	_, err := os.Lstat(filepath.Join(worktreePath, ".git"))
	return os.IsNotExist(err)
}

// removeWorktreeWindows tears down a runtime: windows repo's worktree: the
// real git worktree (resolved via the WSL-side symlink) is deregistered
// through Windows-native git.exe, then the symlink itself is removed. If we
// only removed the symlink, the real Windows-native tree and its worktree
// registration in windows_base would silently leak.
func removeWorktreeWindows(repo *RepoConfig, worktreePath string) error {
	if repo.WindowsBase == "" {
		return fmt.Errorf("repo %s is runtime: windows but has no windows_base", repo.Name)
	}
	winBaseWin, err := wslToWindowsPath(expandHome(repo.WindowsBase))
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}

	realWorktree, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		// Symlink is already gone/broken; nothing left to deregister on the
		// Windows side, just fall through to removing whatever's here.
		fmt.Fprintf(os.Stderr, "warning: resolving symlink %s: %v\n", worktreePath, err)
	} else {
		winWorktreeWin, err := wslToWindowsPath(realWorktree)
		if err != nil {
			return fmt.Errorf("resolving worktree path: %w", err)
		}
		if err := winGitWorktreeRemove(winBaseWin, winWorktreeWin); err != nil {
			return fmt.Errorf("git worktree remove: %w", err)
		}
	}

	if err := os.Remove(worktreePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing symlink %s: %w", worktreePath, err)
	}
	return nil
}
