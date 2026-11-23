// Package hooks discovers and runs the user's hook executables under
// <config dir>/hooks/<phase>/ (see docs/CONTRACT.md). fleet-task runs the
// create/remove phases, fleet-run the run phases. A failing hook only
// warns: it never aborts or vetoes the operation.
package hooks

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/JonGanz/fleet/internal/cli"
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

// Run runs each hook in <config dir>/hooks/<phase>/ in filename-sorted
// order, with env's variables added to the inherited environment. A hook's
// stdout goes to stdout and its stderr to os.Stderr. A hook exiting
// non-zero (or failing to start) prints a warning and the next hook still
// runs. The returned error is only for failing to find the hooks.
func Run(phase string, env Env, stdout io.Writer) error {
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
		if err := cmd.Run(); err != nil {
			cli.Warnf("%s hook %s failed for %s: %v", phase, path, env.label(), err)
		}
	}
	return nil
}
