package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
	"github.com/JonGanz/fleet/internal/proc"
)

// batchModeSSH is the GIT_SSH_COMMAND fleet-task sets for WSL-side git when
// no prompt may be shown and the user hasn't configured ssh for git: an ssh
// that would ask for a passphrase or host-key confirmation fails at once
// instead of hanging until the network timeout.
const batchModeSSH = "ssh -o BatchMode=yes"

// linuxGit is the gitRunner for runtime: linux repos: plain WSL/Linux-native
// `git` against WSL-side paths. cfg supplies the network timeout; nil (no
// usable repos.yaml) means the default.
type linuxGit struct {
	cfg *config.ReposConfig
}

// sshAgentCheck makes checkSSHAgent run at most once per process: one
// warning is enough even when several SSH repos are fetched.
var sshAgentCheck sync.Once

// batchModeSSHEnv returns the extra environment for a WSL-side network git
// command: GIT_SSH_COMMAND=batchModeSSH when prompts aren't allowed and the
// user has set none of GIT_SSH_COMMAND, GIT_SSH (which GIT_SSH_COMMAND would
// silently override) or core.sshCommand; nil otherwise. A variable that is
// set at all, even empty, counts as the user's choice, and so does a
// core.sshCommand lookup that fails: never override what might be theirs.
// lookupEnv and coreSSHCommandSet are injected for testing.
func batchModeSSHEnv(interactive bool, lookupEnv func(string) (string, bool), coreSSHCommandSet func() (bool, error)) []string {
	if interactive {
		return nil
	}
	for _, name := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		if _, set := lookupEnv(name); set {
			return nil
		}
	}
	if set, err := coreSSHCommandSet(); err != nil || set {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=" + batchModeSSH}
}

// checkSSHAgent runs `ssh-add -l` and prints a warning to stderr if it
// exits non-zero (no identities loaded / agent unreachable). This is
// advisory only -- some setups use agent forwarding this check can't see,
// so we never block on the result.
func checkSSHAgent() {
	cmd := exec.Command("ssh-add", "-l")
	out, err := cmd.CombinedOutput()
	if err != nil {
		cli.Warnf("ssh-add -l failed (%v); no SSH identities may be loaded%s", err, stderrSuffix(strings.Join(strings.Fields(string(out)), " ")))
	}
}

// copyFile copies src to dst (created or truncated).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// coreSSHCommandSet reports whether core.sshCommand is configured for base
// (`git config --get` also sees the global and system config).
func coreSSHCommandSet(base string) (bool, error) {
	v, err := gitOutput(nil, "-C", base, "config", "--get", "core.sshCommand")
	if err != nil {
		if gitExitCode(err) == 1 { // 1: key not set
			return false, nil
		}
		return false, err
	}
	return v != "", nil
}

// gitEnv is the environment for WSL-side git. When prompting isn't allowed
// (no terminal, or FLEET_NO_INPUT=1) GIT_TERMINAL_PROMPT=0 makes an HTTPS
// credential prompt fail instead of hanging an agent forever. extra entries
// (KEY=value) are appended last, so they win.
func gitEnv(extra ...string) []string {
	env := os.Environ()
	if !cli.InteractiveAllowed() {
		env = append(env, "GIT_TERMINAL_PROMPT=0")
	}
	return append(env, extra...)
}

// gitExitCode returns the exit code carried by an error from the git
// helpers below, or -1 if git didn't run to an exit status (e.g. missing
// binary).
func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// gitOutput runs `git <args...>` and returns its trimmed stdout. stderr is
// captured into the returned error (which wraps the *exec.ExitError, see
// gitExitCode). env adds KEY=value entries to gitEnv's.
func gitOutput(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv(env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w%s", strings.Join(args, " "), err, stderrSuffix(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// AddOrigin runs `git -C <base> remote add origin <origin>`.
func (linuxGit) AddOrigin(base, origin string) error {
	return runGit("-C", base, "remote", "add", "origin", origin)
}

// AdoptWorktree needs nothing more for a linux worktree: it is a plain
// directory, already where it belongs.
func (linuxGit) AdoptWorktree(string) error { return nil }

// ApplyPatch runs `git -C <worktreePath> apply <patchFile>`.
//
// We use `apply` rather than `am` for v1: `am` expects RFC 2822-style
// commit-message headers (as produced by `git format-patch`) and will
// fail/hang on a plain `git diff` output, whereas `apply` works uniformly
// for both patch styles as long as the patch doesn't need to *create* a
// commit. Since applied patches here are just working-tree changes ahead
// of the user's own commits in the new worktree, `apply` is simpler and
// sufficient; not preserving patch authorship/message metadata is an
// accepted trade-off for v1.
func (linuxGit) ApplyPatch(worktreePath, patchFile string) error {
	return runGit("-C", worktreePath, "apply", patchFile)
}

// Changes compares two trees built with throwaway index files, so the
// worktree and its real index are never touched (no stash, no add):
//
//   - expected: HEAD plus the recorded patches (`read-tree HEAD`, then
//     `apply --cached` each patch, `write-tree`);
//   - actual: the worktree as it is (`add -A` into a copy of the real index
//     -- the copy keeps its stat cache, so unchanged files aren't re-hashed
//     -- then `write-tree`). `add -A` honours .gitignore.
//
// `diff-tree` between them lists exactly the changes the patches don't
// account for. If a patch no longer applies to HEAD (e.g. the user
// committed over it), expected falls back to plain HEAD and the report says
// patches were not excluded.
func (linuxGit) Changes(worktreePath string, patchFiles []string, exclude bool) changeReport {
	tmp, err := os.MkdirTemp("", "fleet-task-changes-")
	if err != nil {
		return unknownChanges(fmt.Sprintf("temp dir: %v", err))
	}
	defer os.RemoveAll(tmp)
	git := func(index string, args ...string) (string, error) {
		var env []string
		if index != "" {
			env = []string{"GIT_INDEX_FILE=" + index}
		}
		return gitOutput(env, append([]string{"-C", worktreePath}, args...)...)
	}

	headTree, err := git("", "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return unknownChanges(err.Error())
	}

	expected, excluded, reason := headTree, exclude, ""
	if exclude && len(patchFiles) > 0 {
		tree, err := treeWithPatches(git, filepath.Join(tmp, "expected.index"), patchFiles)
		if err != nil {
			excluded = false
			reason = fmt.Sprintf("%v; patch changes are included", err)
		} else {
			expected = tree
		}
	}

	actual, err := worktreeTree(git, worktreePath, filepath.Join(tmp, "actual.index"))
	if err != nil {
		return unknownChanges(err.Error())
	}

	// gitOutput's trimming is safe here: -z output starts with a status
	// letter and ends with a NUL, never with whitespace.
	diff, err := git("", "diff-tree", "-r", "--name-status", "-z", expected, actual)
	if err != nil {
		return unknownChanges(err.Error())
	}
	return reportFromFiles(parseNameStatus(diff), excluded, false, reason)
}

// DeleteBranch runs `git -C <base> branch -D <branch>`, stderr captured
// into the error (git's "checked out at ..." refusal is what gets reported).
func (linuxGit) DeleteBranch(base, branch string) error {
	return runGitCapture("-C", base, "branch", "-D", branch)
}

// Fetch runs `git -C <base> fetch --prune origin` (prune drops
// origin/<branch> refs deleted upstream, so a stale one can't be picked up
// by planWorktreeAdd), killed after git.network_timeout. For an SSH origin it
// first warns, once per process, if no ssh-agent identity is loaded. Without
// a terminal ssh runs in BatchMode unless the user configured ssh for git
// (batchModeSSHEnv).
func (g linuxGit) Fetch(base, origin string) error {
	if isSSHURL(origin) {
		sshAgentCheck.Do(checkSSHAgent)
	}
	env := batchModeSSHEnv(cli.InteractiveAllowed(), os.LookupEnv, func() (bool, error) {
		return coreSSHCommandSet(base)
	})
	return runGitNetwork(g.cfg.NetworkTimeout(), env, "-C", base, "fetch", "--prune", "origin")
}

// InitBare runs `git init --bare <base>` (creating parent dirs). Safe on an
// existing empty directory left by an interrupted run.
//
// init + `remote add` + fetch rather than `git clone --bare`: clone --bare
// copies every remote branch into the repo's own refs/heads/* (a pile of
// local branches that go stale, since later fetches only update origin/*)
// and does not set remote.origin.fetch. `remote add` sets the standard
// `+refs/heads/*:refs/remotes/origin/*` refspec, so the base has no local
// branches until a worktree creates one.
func (linuxGit) InitBare(base string) error {
	if err := os.MkdirAll(parentDir(base), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parentDir(base), err)
	}
	return runGit("init", "--bare", base)
}

// InspectBase reports what is at base. "Is a repo" means base itself is the
// git dir ("." for a bare repo) or a regular clone's top level (".git") --
// `rev-parse` run inside a plain directory that merely sits inside some
// other repo would otherwise report that repo.
func (linuxGit) InspectBase(base string) (baseInfo, error) {
	fi, err := os.Stat(base)
	if os.IsNotExist(err) {
		return baseInfo{}, nil
	}
	if err != nil {
		return baseInfo{}, err
	}
	if !fi.IsDir() {
		return baseInfo{}, fmt.Errorf("%s exists but is not a directory", base)
	}
	info := baseInfo{Exists: true}
	gitDir, err := gitOutput(nil, "-C", base, "rev-parse", "--git-dir")
	if err == nil && (gitDir == "." || gitDir == ".git") {
		info.IsRepo = true
	} else {
		if err != nil {
			info.Detail = err.Error()
		} else {
			info.Detail = "it is inside the repository at " + gitDir
		}
		entries, rerr := os.ReadDir(base)
		if rerr != nil {
			return baseInfo{}, rerr
		}
		info.Empty = len(entries) == 0
		return info, nil
	}
	url, err := gitOutput(nil, "-C", base, "config", "--get", "remote.origin.url")
	if err != nil && gitExitCode(err) != 1 { // 1: key not set
		return baseInfo{}, err
	}
	info.OriginURL = url
	return info, nil
}

// InspectWorktreePath checks worktreePath against `git worktree list
// --porcelain` in base. Paths are matched by file identity (os.SameFile),
// so symlinks or a differently spelled path to the same directory still
// match.
func (linuxGit) InspectWorktreePath(base, worktreePath string) (worktreeAtPath, error) {
	fi, err := os.Stat(worktreePath)
	if os.IsNotExist(err) {
		if _, lerr := os.Lstat(worktreePath); lerr == nil {
			// A dangling symlink: something is there, but no worktree.
			return worktreeAtPath{Exists: true, Found: describeForeignPath(worktreePath, base)}, nil
		}
		return worktreeAtPath{}, nil
	}
	if err != nil {
		return worktreeAtPath{}, err
	}
	at := worktreeAtPath{Exists: true}
	list, err := gitOutput(nil, "-C", base, "worktree", "list", "--porcelain")
	if err != nil {
		return worktreeAtPath{}, err
	}
	for _, e := range parseWorktreeList(list) {
		if e.Bare {
			continue
		}
		if efi, err := os.Stat(e.Path); err == nil && os.SameFile(fi, efi) {
			at.Registered, at.Ref = true, e.Ref
			return at, nil
		}
	}
	at.Found = describeForeignPath(worktreePath, base)
	return at, nil
}

func (linuxGit) Preflight(base, worktreePath string) error { return nil }

// Prune runs `git -C <base> worktree prune`, clearing any
// registered-but-missing worktree entries left behind by an earlier
// interrupted run or a worktree directory removed without `git worktree
// remove`. Safe to call unconditionally: a no-op when nothing is stale.
func (linuxGit) Prune(base string) error {
	return runGit("-C", base, "worktree", "prune")
}

// RefExists runs `git show-ref --verify --quiet <ref>`: exit 1 means absent.
func (linuxGit) RefExists(base, ref string) (bool, error) {
	_, err := gitOutput(nil, "-C", base, "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}
	if gitExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// UnpushedCommits runs `git rev-list --count refs/heads/<branch> --not
// --remotes` in the worktree.
func (linuxGit) UnpushedCommits(worktreePath, branch string) (int, error) {
	s, err := gitOutput(nil, "-C", worktreePath, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// UnpushedInBase runs `git rev-list --count refs/heads/<branch> --not
// --remotes` in base.
func (linuxGit) UnpushedInBase(base, branch string) (int, error) {
	s, err := gitOutput(nil, "-C", base, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// WorktreeAdd runs `git -C <base> worktree add ...` per plan.
func (linuxGit) WorktreeAdd(base, worktreePath string, plan worktreeAddPlan) error {
	if err := os.MkdirAll(parentDir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parentDir(worktreePath), err)
	}
	return runGitCapture(append([]string{"-C", base}, plan.args(worktreePath)...)...)
}

// WorktreeMove runs `git -C <base> worktree move <oldPath> <newPath>`,
// relocating a worktree while keeping git's own worktree administrative
// data (the .git file inside it, and base's internal worktree registration)
// consistent -- unlike a plain filesystem rename, which would leave git
// still pointing at the old location.
func (linuxGit) WorktreeMove(base, oldPath, newPath string) error {
	if err := os.MkdirAll(parentDir(newPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parentDir(newPath), err)
	}
	return runGit("-C", base, "worktree", "move", oldPath, newPath)
}

// WorktreeRemove runs `git -C <base> worktree remove <worktreePath> --force`.
func (linuxGit) WorktreeRemove(base, worktreePath string) error {
	return runGit("-C", base, "worktree", "remove", worktreePath, "--force")
}

// runGit runs `git <args...>` with stdout/stderr inherited, returning an
// error including combined context on failure.
func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv()
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}

// runGitCapture is runGit for commands whose failure the caller reports
// itself: stderr is captured (not shown live) and included in the returned
// error, so a failure reads "git worktree add ...: exit status 128: fatal:
// invalid reference: origin/mian" instead of a bare "exit status 128".
func runGitCapture(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w%s", strings.Join(args, " "), err, stderrSuffix(stderr.String()))
	}
	return nil
}

// runGitNetwork runs a git command that talks to a remote (stdout/stderr
// inherited like runGit) and kills it after timeout. Without a terminal the
// command gets its own process group, so the kill takes its ssh (or
// credential helper) along; with a terminal it stays in ours, so ssh and
// HTTPS prompts can still read the terminal and Ctrl-C reaches it directly.
// env adds KEY=value entries to gitEnv's.
func runGitNetwork(timeout time.Duration, env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv(env...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	err := proc.Run(cmd, proc.Options{Timeout: timeout, NewGroup: !cli.InteractiveAllowed()})
	if proc.IsTimeout(err) {
		return fmt.Errorf("git %s: %w (set git.network_timeout in repos.yaml if the network is just slow)", strings.Join(args, " "), err)
	}
	if err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}

// stderrSuffix formats captured stderr for appending to an error message.
func stderrSuffix(stderr string) string {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return ""
	}
	return ": " + s
}

// treeWithPatches builds HEAD + patchFiles in a throwaway index and returns
// the tree id. The error names the patch that no longer applies.
func treeWithPatches(git func(index string, args ...string) (string, error), index string, patchFiles []string) (string, error) {
	if _, err := git(index, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	for _, p := range patchFiles {
		if _, err := git(index, "apply", "--cached", p); err != nil {
			return "", fmt.Errorf("recorded patch %s no longer applies to HEAD", filepath.Base(p))
		}
	}
	return git(index, "write-tree")
}

// worktreeTree snapshots the worktree into a throwaway index and returns
// its tree id. It starts from a copy of the real index (for its stat cache)
// and falls back to a fresh `read-tree HEAD` if the copy can't be used
// (e.g. split-index setups whose shared part lives next to the original).
func worktreeTree(git func(index string, args ...string) (string, error), worktreePath, index string) (string, error) {
	if realIndex, err := git("", "rev-parse", "--git-path", "index"); err == nil {
		if !filepath.IsAbs(realIndex) {
			realIndex = filepath.Join(worktreePath, realIndex)
		}
		if copyFile(realIndex, index) == nil {
			if _, err := git(index, "add", "-A"); err == nil {
				if tree, err := git(index, "write-tree"); err == nil {
					return tree, nil
				}
			}
		}
	}
	os.Remove(index)
	if _, err := git(index, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := git(index, "add", "-A"); err != nil {
		return "", err
	}
	return git(index, "write-tree")
}
