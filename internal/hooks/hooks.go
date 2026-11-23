// Package hooks discovers and runs the user's hook executables under
// <config dir>/hooks/<phase>/ (see docs/CONTRACT.md). fleet-task runs the
// create/remove phases, fleet-run the run phases. A failing pre-* hook
// vetoes the one repo/window it ran for (Run returns an *Error); a failing
// post-* hook only warns. Every hook is killed after a timeout.
package hooks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/proc"
	"github.com/JonGanz/fleet/internal/xdg"
)

// Hook phases, each a directory under <config dir>/hooks/.
const (
	PreCreate  = "pre-create"
	PostCreate = "post-create"
	PreRemove  = "pre-remove"
	PostRemove = "post-remove"
	PreRun     = "pre-run"
	PostRun    = "post-run"
)

// Discover returns the executable regular files directly in dir, in
// filename-sorted order. Non-executable files and directories are skipped;
// symlinks are followed, so a linked script counts. A missing dir means no
// hooks (nil, nil).
func Discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var hooks []string
	for _, name := range names {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		hooks = append(hooks, p)
	}
	return hooks, nil
}

// Env is what a hook is told about the repo (and, for run hooks, the run
// target) it runs for, exported to it as FLEET_* environment variables.
type Env struct {
	Ticket      string
	Description string
	Repo        string
	Branch      string
	WorktreeDir string
	// RunName is set for pre-run/post-run hooks only.
	RunName string
}

// Vars returns the FLEET_* variables for e. FLEET_RUN_NAME is included only
// when RunName is set.
func (e Env) Vars() []string {
	vars := []string{
		"FLEET_TICKET=" + e.Ticket,
		"FLEET_DESCRIPTION=" + e.Description,
		"FLEET_REPO=" + e.Repo,
		"FLEET_BRANCH=" + e.Branch,
		"FLEET_WORKTREE_DIR=" + e.WorktreeDir,
	}
	if e.RunName != "" {
		vars = append(vars, "FLEET_RUN_NAME="+e.RunName)
	}
	return vars
}

// label names what the hook ran for in warnings: "repo" or "repo:run".
func (e Env) label() string {
	if e.RunName != "" {
		return e.Repo + ":" + e.RunName
	}
	return e.Repo
}

// Error is a pre-* hook that vetoed the operation: it exited non-zero, was
// killed by the timeout, or could not be started.
type Error struct {
	Phase string
	Path  string
	Err   error
}

// Error names the phase, the hook and what went wrong, e.g. "pre-create
// hook /x/10-check failed: exit status 3".
func (e *Error) Error() string {
	return fmt.Sprintf("%s hook %s failed: %v", e.Phase, e.Path, e.Err)
}

// Unwrap returns the underlying exec, start or timeout error.
func (e *Error) Unwrap() error { return e.Err }

// IsVeto reports whether err is (or wraps) a vetoing hook failure.
func IsVeto(err error) bool {
	var he *Error
	return errors.As(err, &he)
}

// Run runs each hook in <config dir>/hooks/<phase>/ in filename-sorted
// order, with env's variables added to the inherited environment and no
// stdin. A hook's stdout goes to stdout and its stderr to os.Stderr. Each
// hook runs in its own process group and is killed, group and all, once it
// has run for timeout (0: no limit), so a hung hook can't block the command
// forever and its children don't outlive it.
//
// For a pre-* phase (Vetoes) the first hook that exits non-zero, times out
// or can't be started stops the phase: the hooks after it don't run, and Run
// returns an *Error naming it, which the caller treats as "cancel this
// repo/window". For a post-* phase a failure prints a warning and the next
// hook still runs; Run then returns nil. A failure to find the hooks is
// returned for every phase (a pre-* caller cancels on it too: it can't know
// whether a hook would have vetoed).
func Run(phase string, env Env, stdout io.Writer, timeout time.Duration) error {
	dir, err := xdg.HooksDir(phase)
	if err != nil {
		return err
	}
	hooks, err := Discover(dir)
	if err != nil {
		return err
	}
	environ := append(os.Environ(), env.Vars()...)
	for _, path := range hooks {
		cmd := exec.Command(path)
		cmd.Env = environ
		cmd.Stdout = stdout
		cmd.Stderr = os.Stderr
		err := proc.Run(cmd, proc.Options{Timeout: timeout, NewGroup: true})
		if err == nil {
			continue
		}
		if Vetoes(phase) {
			return &Error{Phase: phase, Path: path, Err: err}
		}
		cli.Warnf("%s hook %s failed for %s: %v", phase, path, env.label(), err)
	}
	return nil
}

// Vetoes reports whether a failing hook of phase cancels the operation it
// runs before: true for the pre-* phases.
func Vetoes(phase string) bool {
	return strings.HasPrefix(phase, "pre-")
}
