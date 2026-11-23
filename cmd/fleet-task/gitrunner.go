package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/xdg"
)

// gitRunner abstracts "how git is driven" for one repo's runtime: linuxGit
// runs WSL-native git against WSL paths, windowsGit runs Windows-native
// git.exe (via powershell.exe) against win32 paths, with the real worktree
// on the NTFS side and a WSL-side symlink at the normal worktree path.
//
// Every path argument is in WSL-visible form (the convention used throughout
// repos.yaml and the state files); the windows implementation converts to
// win32 form itself. This keeps callers (new/edit/rm/mv) free of per-runtime
// switches. The decisions on top of these primitives (ensureBase,
// addWorktree) are shared and runtime-independent.
type gitRunner interface {
	// Preflight validates a base/worktree pair before any work starts.
	Preflight(base, worktreePath string) error
	// InspectBase reports what is currently at base (see baseInfo).
	InspectBase(base string) (baseInfo, error)
	// InitBare runs `git init --bare` at base, creating parent dirs.
	InitBare(base string) error
	// AddOrigin runs `git remote add origin <origin>` in base.
	AddOrigin(base, origin string) error
	// Fetch runs `git fetch --prune origin` in base. origin is repos.yaml's
	// URL, used only for pre-fetch checks (the SSH agent warning).
	Fetch(base, origin string) error
	// Prune clears stale worktree registrations (`git worktree prune`).
	Prune(base string) error
	// RefExists reports whether the full ref (e.g. refs/heads/x) exists in
	// base.
	RefExists(base, ref string) (bool, error)
	// WorktreeAdd creates a worktree at worktreePath as plan says.
	WorktreeAdd(base, worktreePath string, plan worktreeAddPlan) error
	WorktreeRemove(base, worktreePath string) error
	WorktreeMove(base, oldPath, newPath string) error
	ApplyPatch(worktreePath, patchFile string) error
	// Changes reports the worktree's uncommitted changes; with exclude,
	// minus those the given (recorded, applied) patch files account for.
	// It never modifies the worktree or its index. Failures are reported as
	// an "unknown" changeReport, never as clean.
	Changes(worktreePath string, patchFiles []string, exclude bool) changeReport
	// UnpushedCommits counts commits on branch that are on no remote.
	UnpushedCommits(worktreePath, branch string) (int, error)
	// UnpushedInBase is UnpushedCommits run in base rather than in a
	// worktree (which may already be gone): commits on the local branch
	// reachable from no remote-tracking ref.
	UnpushedInBase(base, branch string) (int, error)
	// DeleteBranch force-deletes the local branch in base (`git branch -D`).
	// Callers decide first whether that is safe; git itself still refuses a
	// branch checked out in some worktree.
	DeleteBranch(base, branch string) error
	// InspectWorktreePath reports what is at worktreePath before a worktree
	// is created there, and whether it is a registered worktree of base.
	InspectWorktreePath(base, worktreePath string) (worktreeAtPath, error)
	// AdoptWorktree finishes adopting the registered worktree at
	// worktreePath (windows: recreates a missing WSL-side symlink).
	AdoptWorktree(worktreePath string) error
}

// worktreeAtPath is what InspectWorktreePath found at a worktree path.
type worktreeAtPath struct {
	// Exists: something is there (for windows: the WSL-side link, or the
	// real tree registered in base).
	Exists bool
	// Registered: it is a worktree registered in this repo's base.
	Registered bool
	// Ref is a registered worktree's checked-out ref (refs/heads/<x>), or
	// "" for a detached HEAD.
	Ref string
	// Found describes what is there when it is not a registered worktree,
	// for the error message.
	Found string
}

// worktreeEntry is one record of `git worktree list --porcelain`.
type worktreeEntry struct {
	Path string
	Ref  string // "branch" line (refs/heads/<x>); "" if detached or bare
	Bare bool
}

// baseInfo describes what is at a repo's base path before setup.
type baseInfo struct {
	Exists bool
	// IsRepo: base is itself a git repository (bare, or a regular clone's
	// top level) -- not merely a directory inside some other repo.
	IsRepo bool
	// Empty: base exists, is not a repo, and has no entries.
	Empty bool
	// OriginURL is remote.origin.url, or "" if there is no origin remote.
	OriginURL string
	// Detail is git's explanation when base exists but isn't a repo.
	Detail string
}

// baseSetup is what ensureBase must do before fetching.
type baseSetup struct {
	Init      bool
	AddOrigin bool
	Warning   string
}

// Worktree-add modes, chosen by planWorktreeAdd.
const (
	worktreeAttach = "attach" // local branch exists: check it out
	worktreeTrack  = "track"  // only origin/<branch> exists: create a local branch tracking it
	worktreeCreate = "create" // neither exists: create from origin/<default_branch>, no upstream
)

// worktreeAddPlan is how to create one worktree; see planWorktreeAdd.
type worktreeAddPlan struct {
	Mode       string
	Branch     string
	StartPoint string // "" for attach
}

// addWorktree decides how to create the worktree (planWorktreeAdd, probing
// base's refs) and creates it, wrapping a failure with what was attempted.
func addWorktree(r gitRunner, base, worktreePath, branch, defaultBranch string) error {
	plan, err := planWorktreeAdd(branch, defaultBranch, func(ref string) (bool, error) {
		return r.RefExists(base, ref)
	})
	if err != nil {
		return err
	}
	if err := r.WorktreeAdd(base, worktreePath, plan); err != nil {
		return fmt.Errorf("%s: %w", plan.describe(), err)
	}
	return nil
}

// args returns git's arguments (after any -C) for the plan, with every
// option before the path so git never mistakes the path for an option's
// value.
func (p worktreeAddPlan) args(worktreePath string) []string {
	switch p.Mode {
	case worktreeAttach:
		return []string{"worktree", "add", worktreePath, p.Branch}
	case worktreeTrack:
		return []string{"worktree", "add", "--track", "-b", p.Branch, worktreePath, p.StartPoint}
	default:
		return []string{"worktree", "add", "--no-track", "-b", p.Branch, worktreePath, p.StartPoint}
	}
}

// describe renders the plan for error messages.
func (p worktreeAddPlan) describe() string {
	switch p.Mode {
	case worktreeAttach:
		return fmt.Sprintf("attaching existing local branch %s", p.Branch)
	case worktreeTrack:
		return fmt.Sprintf("creating branch %s tracking %s", p.Branch, p.StartPoint)
	default:
		return fmt.Sprintf("creating branch %s from %s", p.Branch, p.StartPoint)
	}
}

// decideAdoption decides what to do about what InspectWorktreePath found at
// worktreePath before creating a worktree on branch there: nothing there
// means create it (false, nil); a registered worktree of base on that very
// branch -- left by an interrupted `new`/`edit --add` -- is adopted (true,
// nil); anything else is an error naming what was found, never touched.
func decideAdoption(at worktreeAtPath, worktreePath, base, branch string) (bool, error) {
	switch {
	case !at.Exists:
		return false, nil
	case at.Registered && at.Ref == "refs/heads/"+branch:
		return true, nil
	case at.Registered && at.Ref == "":
		return false, fmt.Errorf("%s is already a worktree of %s, but with a detached HEAD instead of branch %s; check it out there (`git -C %s switch %s`) or remove it, then retry", worktreePath, base, branch, worktreePath, branch)
	case at.Registered:
		return false, fmt.Errorf("%s is already a worktree of %s, but on %s instead of branch %s; remove it or switch it to %s, then retry", worktreePath, base, strings.TrimPrefix(at.Ref, "refs/heads/"), branch, branch)
	}
	found := at.Found
	if found == "" {
		found = "something that is not a worktree of " + base
	}
	return false, fmt.Errorf("%s already exists and is %s; move it away, then retry", worktreePath, found)
}

// describeForeignPath says what is at path (which exists but is not a
// registered worktree of base), for decideAdoption's error. It only looks;
// it never runs git inside a directory it doesn't know.
func describeForeignPath(path, base string) string {
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		return fmt.Sprintf("something that can't be inspected (%v)", err)
	case fi.Mode()&os.ModeSymlink != 0:
		target, _ := os.Readlink(path)
		return fmt.Sprintf("a symlink to %s, not a worktree of %s", target, base)
	case !fi.IsDir():
		return "a file, not a worktree of " + base
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
		return "a git checkout that is not a worktree of " + base + " (another repo, or a worktree of another base)"
	}
	return "a directory that is not a git worktree of " + base
}

// ensureBase makes base a usable repo with an origin remote and fetches it.
// It is idempotent: a base left half-made by an interrupted first run (an
// empty dir, or `init --bare` done but no remote yet) is completed rather
// than failing on fetch forever.
func ensureBase(r gitRunner, repoName, origin, base string) error {
	info, err := r.InspectBase(base)
	if err != nil {
		return fmt.Errorf("checking base %s: %w", base, err)
	}
	setup, err := planBaseSetup(info, repoName, base, origin)
	if err != nil {
		return err
	}
	if setup.Warning != "" {
		cli.Warnf("%s", setup.Warning)
	}
	if setup.Init {
		if err := r.InitBare(base); err != nil {
			return fmt.Errorf("init bare clone %s: %w", base, err)
		}
	}
	if setup.AddOrigin {
		if err := r.AddOrigin(base, origin); err != nil {
			return fmt.Errorf("adding origin %s to %s: %w", origin, base, err)
		}
	}
	if err := r.Fetch(base, origin); err != nil {
		return fmt.Errorf("fetch %s: %w", base, err)
	}
	return nil
}

// gitFor returns the gitRunner for repo along with its base clone path in
// WSL-visible form. For linux repos base may be "" (repos.yaml has neither
// `base` nor defaults.base_root); callers decide whether that's fatal (new)
// or best-effort (rm, mv).
func gitFor(cfg *config.ReposConfig, repo *config.RepoConfig) (gitRunner, string, error) {
	if repo.Runtime == "windows" {
		if repo.WindowsBase == "" {
			return nil, "", fmt.Errorf("repo %s is runtime: windows but has no windows_base (set it directly or via defaults.windows_base_root)", repo.Name)
		}
		return windowsGit{cfg: cfg}, xdg.ExpandHome(repo.WindowsBase), nil
	}
	return linuxGit{cfg: cfg}, xdg.ExpandHome(repo.Base), nil
}

// gitForExisting is gitFor for operating on an already-created worktree
// (rm, mv): if the repo is missing from repos.yaml, or has no usable base,
// it falls back to a linux runner whose "base" is the worktree itself --
// any worktree checkout has a .git file pointing back at its base repo, so
// `git -C <worktree> worktree remove|move` still works.
func gitForExisting(cfg *config.ReposConfig, repoName, worktreePath, what string) (gitRunner, string, error) {
	r, base, known, err := resolveExistingGit(cfg, repoName, worktreePath)
	if err != nil {
		return nil, "", err
	}
	if !known {
		cli.Warnf("repo %q not found in repos.yaml (or has no base), trying %s without a base", repoName, what)
	}
	return r, base, nil
}

// parentDir is filepath.Dir, named for readability at call sites.
func parentDir(p string) string { return filepath.Dir(p) }

// parseWorktreeList parses `git worktree list --porcelain`: records
// separated by blank lines, each starting with "worktree <path>", then
// "HEAD <sha>", "branch <ref>", "detached", "bare", "locked", "prunable"
// lines. \r is tolerated (git.exe output through PowerShell).
func parseWorktreeList(out string) []worktreeEntry {
	var entries []worktreeEntry
	var cur *worktreeEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		key, value, _ := strings.Cut(line, " ")
		switch {
		case key == "worktree":
			entries = append(entries, worktreeEntry{Path: value})
			cur = &entries[len(entries)-1]
		case cur == nil:
			continue
		case key == "branch":
			cur.Ref = value
		case key == "bare":
			cur.Bare = true
		}
	}
	return entries
}

// planBaseSetup decides what ensureBase must do for what InspectBase found.
// A non-empty directory that isn't a repo is never initialised over (it may
// hold someone's files). An origin URL that differs from repos.yaml's is
// warned about but left alone: rewriting it silently could point a shared
// base at a different repository.
func planBaseSetup(info baseInfo, repoName, base, origin string) (baseSetup, error) {
	switch {
	case !info.Exists || (!info.IsRepo && info.Empty):
		if origin == "" {
			return baseSetup{}, fmt.Errorf("repo %s has no origin in repos.yaml, so its base %s can't be created", repoName, base)
		}
		return baseSetup{Init: true, AddOrigin: true}, nil
	case !info.IsRepo:
		detail := ""
		if info.Detail != "" {
			detail = " (" + info.Detail + ")"
		}
		return baseSetup{}, fmt.Errorf("base %s for repo %s exists but is not a git repository%s; move it away or fix `base` in repos.yaml", base, repoName, detail)
	case info.OriginURL == "":
		if origin == "" {
			return baseSetup{}, fmt.Errorf("base %s has no origin remote and repo %s has no origin in repos.yaml", base, repoName)
		}
		return baseSetup{AddOrigin: true}, nil
	case origin != "" && info.OriginURL != origin:
		return baseSetup{Warning: fmt.Sprintf(
			"base %s has origin %s but repos.yaml says %s for repo %s; leaving it as is (to switch: git -C %s remote set-url origin %s)",
			base, info.OriginURL, origin, repoName, base, origin)}, nil
	}
	return baseSetup{}, nil
}

// resolveExistingGit is gitForExisting without the warning: known is false
// when it fell back to running git in the worktree itself because repos.yaml
// has no usable entry (no base) for repoName.
func resolveExistingGit(cfg *config.ReposConfig, repoName, worktreePath string) (r gitRunner, base string, known bool, err error) {
	if repo := cfg.FindRepo(repoName); repo != nil {
		r, base, err := gitFor(cfg, repo)
		if err != nil {
			return nil, "", false, err
		}
		if base != "" {
			return r, base, true, nil
		}
	}
	return linuxGit{cfg: cfg}, worktreePath, false, nil
}

// planWorktreeAdd chooses how to create a ticket's worktree on branch:
// attach the local branch if it exists (a rerun, or a repo re-added by
// `edit`); else, if origin/<branch> exists (someone pushed it), create the
// local branch tracking it; else create it from origin/<defaultBranch>
// with no upstream (tracking the default branch would make a later plain
// `git push` target it). refExists probes base for a full ref name.
func planWorktreeAdd(branch, defaultBranch string, refExists func(ref string) (bool, error)) (worktreeAddPlan, error) {
	local, err := refExists("refs/heads/" + branch)
	if err != nil {
		return worktreeAddPlan{}, fmt.Errorf("checking for local branch %s: %w", branch, err)
	}
	if local {
		return worktreeAddPlan{Mode: worktreeAttach, Branch: branch}, nil
	}
	remote, err := refExists("refs/remotes/origin/" + branch)
	if err != nil {
		return worktreeAddPlan{}, fmt.Errorf("checking for origin/%s: %w", branch, err)
	}
	if remote {
		return worktreeAddPlan{Mode: worktreeTrack, Branch: branch, StartPoint: "origin/" + branch}, nil
	}
	if defaultBranch == "" {
		return worktreeAddPlan{}, fmt.Errorf("branch %s exists neither locally nor on origin, and no default_branch is configured to create it from (set default_branch or defaults.default_branch in repos.yaml)", branch)
	}
	def, err := refExists("refs/remotes/origin/" + defaultBranch)
	if err != nil {
		return worktreeAddPlan{}, fmt.Errorf("checking for origin/%s: %w", defaultBranch, err)
	}
	if !def {
		return worktreeAddPlan{}, fmt.Errorf("branch %s exists neither locally nor on origin, and default_branch origin/%s does not exist to create it from; check default_branch in repos.yaml", branch, defaultBranch)
	}
	return worktreeAddPlan{Mode: worktreeCreate, Branch: branch, StartPoint: "origin/" + defaultBranch}, nil
}
