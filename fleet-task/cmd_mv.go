package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdMv renames an existing task's ticket id: the state file (and its
// `Ticket` field) and every attached repo's worktree directory move from
// old to new. Branches are left untouched -- a repo's branch name comes
// from a template (branchName) that isn't necessarily equal to the ticket
// string, and renaming a possibly-already-pushed branch is a separate,
// riskier operation than renaming the ticket identity itself.
func cmdMv(oldTicket, newTicket string) error {
	if oldTicket == "" {
		t, err := pickTicket()
		if err != nil {
			return err
		}
		oldTicket = t
	}

	// Same race `rm`/`edit` guard against: renaming a ticket that's live
	// under fleet-run would desync its tmux @fleet_task_ticket option (and
	// fleet-run's own in-memory state) from the new filename, so refuse
	// outright rather than trying to handle it.
	if active, found := activeFleetTicket(); found && active == oldTicket {
		return fmt.Errorf("%s is currently active in fleet-run; run `fleet-run stop --ticket %s` (or `fleet-run stop --all`) before renaming it", oldTicket, oldTicket)
	}

	oldFile, err := taskFile(oldTicket)
	if err != nil {
		return err
	}
	st, err := readTaskState(oldFile)
	if err != nil {
		return fmt.Errorf("read task %s: %w", oldTicket, err)
	}

	if newTicket == "" {
		values, err := promptForm([]string{"New ticket"})
		if err != nil {
			return fmt.Errorf("read new ticket: %w", err)
		}
		newTicket = values[0]
	}
	if err := validateTicketName(newTicket); err != nil {
		return err
	}
	if newTicket == oldTicket {
		return fmt.Errorf("new ticket %q is the same as the old ticket", newTicket)
	}

	newFile, err := taskFile(newTicket)
	if err != nil {
		return err
	}
	if _, err := os.Stat(newFile); err == nil {
		return fmt.Errorf("a task already exists at %s", newFile)
	}

	var cfg *ReposConfig
	if rf, err := reposFile(); err == nil {
		if c, err := loadReposConfig(rf); err == nil {
			cfg = c
		}
	}

	wtRoot, err := worktreeRoot(cfg)
	if err != nil {
		return err
	}
	oldTicketDir := filepath.Join(wtRoot, oldTicket)
	newTicketDir := filepath.Join(wtRoot, newTicket)
	if _, err := os.Stat(newTicketDir); err == nil {
		return fmt.Errorf("worktree directory %s already exists", newTicketDir)
	}

	// Move every repo's worktree before touching any state file: a
	// half-moved task (some repos already relocated, some not) is worse to
	// persist under either ticket name than to just report and let the
	// remaining repos be finished by hand, so nothing is written until every
	// repo below has succeeded.
	movedRepos := make([]TaskRepo, 0, len(st.Repos))
	for i, r := range st.Repos {
		newWorktreePath := filepath.Join(newTicketDir, r.Repo)
		if err := moveRepoWorktree(cfg, oldTicket, newTicket, r, newWorktreePath); err != nil {
			remaining := st.Repos[i+1:]
			return fmt.Errorf(
				"moving worktree %s (%s): %w\n%d of %d repo(s) already moved to %s; %d repo(s) (%s) were not -- finish moving them by hand (e.g. `git worktree move <old> <new>`), then rerun `fleet-task mv %s %s`",
				r.WorktreePath, r.Repo, err, len(movedRepos), len(st.Repos), newTicketDir, len(remaining)+1, repoNames(append([]TaskRepo{r}, remaining...)), oldTicket, newTicket,
			)
		}
		fmt.Printf("moved worktree %s -> %s (%s)\n", r.WorktreePath, newWorktreePath, r.Repo)
		r.WorktreePath = newWorktreePath
		movedRepos = append(movedRepos, r)
	}

	st.Ticket = newTicket
	st.Repos = movedRepos
	if err := writeTaskState(newFile, st); err != nil {
		return fmt.Errorf("write task state %s: %w", newFile, err)
	}
	if err := os.Remove(oldFile); err != nil {
		return fmt.Errorf("remove old task state %s (new state already written to %s): %w", oldFile, newFile, err)
	}
	os.Remove(oldFile + ".lock") // best-effort: stale lockfile from a crashed prior run, if any

	if err := moveTicketDirLeftovers(oldTicketDir, newTicketDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	fmt.Printf("renamed task %s -> %s; state written to %s\n", oldTicket, newTicket, newFile)
	return nil
}

// moveTicketDirLeftovers moves everything still in oldDir after the repo
// worktrees have been relocated (notes, env files, anything the user keeps
// at the ticket root) into newDir, then removes oldDir only if it ended up
// empty. Entries that would collide with something already in newDir are
// left in place, so nothing is ever deleted here.
func moveTicketDirLeftovers(oldDir, newDir string) error {
	entries, err := os.ReadDir(oldDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading old ticket workspace dir %s: %w", oldDir, err)
	}
	if len(entries) > 0 {
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", newDir, err)
		}
	}
	var failed []string
	for _, e := range entries {
		src := filepath.Join(oldDir, e.Name())
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Lstat(dst); err == nil {
			failed = append(failed, fmt.Sprintf("%s (already exists in %s)", e.Name(), newDir))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", e.Name(), err))
			continue
		}
		fmt.Printf("moved %s -> %s\n", src, dst)
	}
	if len(failed) > 0 {
		return fmt.Errorf("left in old ticket workspace dir %s: %s", oldDir, strings.Join(failed, ", "))
	}
	if err := os.Remove(oldDir); err != nil {
		return fmt.Errorf("removing old ticket workspace dir %s: %w", oldDir, err)
	}
	return nil
}

// moveRepoWorktree relocates a single repo's worktree from r.WorktreePath to
// newWorktreePath, using `git worktree move` (not a plain filesystem rename)
// so git's own worktree administrative data stays consistent. cfg may be
// nil, or missing this repo's entry entirely (repos.yaml changed since the
// task was created); best-effort falls back to running the move from
// within the worktree itself rather than aborting, mirroring
// removeRepoWorktree's fallback in cmd_rm.go.
func moveRepoWorktree(cfg *ReposConfig, oldTicket, newTicket string, r TaskRepo, newWorktreePath string) error {
	if err := os.MkdirAll(filepath.Dir(newWorktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(newWorktreePath), err)
	}

	repo := cfg.findRepo(r.Repo)
	switch {
	case repo != nil && repo.Runtime == "windows":
		return moveWorktreeWindows(cfg, repo, oldTicket, newTicket, r.WorktreePath, newWorktreePath)
	case repo != nil && expandHome(repo.Base) != "":
		return gitWorktreeMove(expandHome(repo.Base), r.WorktreePath, newWorktreePath)
	default:
		fmt.Fprintf(os.Stderr, "warning: repo %q not found in repos.yaml, trying worktree move without a base\n", r.Repo)
		return gitWorktreeMove(r.WorktreePath, r.WorktreePath, newWorktreePath)
	}
}

// moveWorktreeWindows is the runtime: windows counterpart of
// moveRepoWorktree: the real git worktree lives under
// windows_worktree_root/<ticket>/<repo> on the Windows-native side and is
// moved there via git.exe, then the WSL-side symlink at worktreePath (what
// every other consumer expects) is recreated pointing at the new location --
// mirroring how removeWorktreeWindows in cmd_rm.go handles teardown for the
// same repo shape.
func moveWorktreeWindows(cfg *ReposConfig, repo *RepoConfig, oldTicket, newTicket, oldWorktreePath, newWorktreePath string) error {
	if repo.WindowsBase == "" {
		return fmt.Errorf("repo %s is runtime: windows but has no windows_base", repo.Name)
	}
	winBaseWin, err := wslToWindowsPath(expandHome(repo.WindowsBase))
	if err != nil {
		return fmt.Errorf("resolving windows_base: %w", err)
	}

	_, oldWinReal, err := windowsWorktreePaths(cfg, repo, oldTicket)
	if err != nil {
		return err
	}
	newWslReal, newWinReal, err := windowsWorktreePaths(cfg, repo, newTicket)
	if err != nil {
		return err
	}

	winNewParent, err := wslToWindowsPath(filepath.Dir(newWslReal))
	if err != nil {
		return fmt.Errorf("resolving new worktree parent: %w", err)
	}
	if err := windowsMkdirAll(winNewParent); err != nil {
		return fmt.Errorf("mkdir %s: %w", winNewParent, err)
	}

	if err := winGitWorktreeMove(winBaseWin, oldWinReal, newWinReal); err != nil {
		return fmt.Errorf("git worktree move: %w", err)
	}

	if err := os.Remove(oldWorktreePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing old symlink %s: %w", oldWorktreePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(newWorktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(newWorktreePath), err)
	}
	if err := os.Symlink(newWslReal, newWorktreePath); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", newWorktreePath, newWslReal, err)
	}
	return nil
}

// validateTicketName rejects ticket ids that can't safely become both a
// JSON filename (<tasks dir>/<ticket>.json) and a worktree directory
// component (<worktree_root>/<ticket>/<repo>).
func validateTicketName(ticket string) error {
	if ticket == "" {
		return fmt.Errorf("ticket id is required")
	}
	if ticket != filepath.Base(ticket) || strings.Contains(ticket, "/") || strings.Contains(ticket, `\`) {
		return fmt.Errorf("ticket id %q must not contain path separators", ticket)
	}
	if ticket == "." || ticket == ".." {
		return fmt.Errorf("ticket id %q is not valid", ticket)
	}
	return nil
}

// repoNames renders repos as a comma-separated list of repo names, for the
// partial-failure error message in cmdMv.
func repoNames(repos []TaskRepo) string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Repo
	}
	return strings.Join(names, ", ")
}
