package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JonGanz/fleet/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=<version>";
// see cli.Version for the fallback when it is left at "dev".
var version = "dev"

const usage = `fleet-cache: node_modules hardlink cache keyed by package-lock.json hash

Usage:
  fleet-cache ensure <dir> [--force] [--cache-root <dir>]
      Ensure <dir>/node_modules is populated by hardlinking from the cache,
      running "npm ci" once per unique package-lock.json hash and reusing it
      across every worktree sharing that lockfile.

      If <dir>/node_modules was already linked from this exact lockfile
      hash (tracked via a <dir>/node_modules/.fleet-cache-hash marker), this
      is a no-op that leaves node_modules untouched -- including anything
      added since, like an ` + "`npm link`" + ` package symlink.

      --force        REBUILD: discard <dir>/node_modules and re-link it from
                     the cache even though the marker already matches
                     (manual changes such as npm links are lost).
      --cache-root   Cache root; overrides $FLEET_CACHE_DIR.

  fleet-cache gc [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <dir>]
      Scan the cache for entries whose package-lock.json hash is no longer
      referenced by any package-lock.json under --roots (searched
      recursively, may be repeated or comma-separated), plus abandoned
      <hash>.tmp-<pid> staging dirs (older than a day).

      Every --roots directory must exist and be readable, or gc errors out
      without deleting anything.

      --force        REALLY DELETE the stale entries. Without it (or without
                     --roots, where nothing counts as referenced) gc is
                     report-only, so it is never destructive by default.
      --json         Print the report as JSON on stdout.
      --cache-root   Cache root; overrides $FLEET_CACHE_DIR.

      Cache root: --cache-root, else $FLEET_CACHE_DIR, else
      $XDG_CACHE_HOME/fleet, else ~/.cache/fleet. Entries live at
      <cache root>/node-cache/<sha256>.

  fleet-cache ensure-windows <wsl-dir> [--force] [--cache-root <wsl-path>]
      Windows-native counterpart of ensure, for runtime: windows repos
      (<wsl-dir> is the worktree's WSL-visible path, e.g. under /mnt/c/...).
      npm ci and the node_modules hardlink population both run as native
      Windows processes (PowerShell/npm), never WSL reaching across the
      9P boundary for bulk work. Skips when already linked, like ensure.

      --force        REBUILD node_modules even if already linked (see ensure).
      --cache-root   WSL-visible Windows cache root (repos.yaml's
                     windows_cache_root), else auto-detected as
                     %LOCALAPPDATA%\fleet. Entries live at
                     <cache root>\node-cache\<sha256>.

  fleet-cache gc-windows [--roots <dir>[,<dir>...]] [--force] [--json] [--cache-root <wsl-path>]
      Windows-native counterpart of gc; same --roots/--force/--json rules,
      scanning/deleting against the Windows-native cache root instead.

  fleet-cache list [--json] [--cache-root <dir>]
      List cache entries (hash, complete or not, last modified, on-disk
      size) and in-progress/abandoned <hash>.tmp-<pid> staging dirs.
      Read-only. Pass a WSL-visible Windows cache root via --cache-root to
      list the Windows-native cache.

      --json         Print the listing as JSON on stdout.
      --cache-root   Cache root; overrides $FLEET_CACHE_DIR.

  fleet-cache version | --version
      Print "fleet-cache <version>".

Flags may appear before or after <dir>. Progress and status messages
(including npm's output) go to stderr; stdout carries only results
(gc's report, list, --json, version).

Exit codes: 0 ok, 1 operation failed (including an interrupted ensure),
2 usage error.
`

// stringSliceFlag accumulates repeated --roots flags in addition to
// supporting comma-separated values within a single occurrence.
type stringSliceFlag struct {
	values []string
}

func (s *stringSliceFlag) String() string {
	return strings.Join(s.values, ",")
}

func (s *stringSliceFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			s.values = append(s.values, part)
		}
	}
	return nil
}

// command describes one subcommand: its flags and positional argument.
type command struct {
	name string
	// posName is the positional placeholder ("" means no positional arg).
	posName string
	// setup registers the command's flags on fs and returns the function
	// that runs it once parsing succeeded (pos holds the positional args).
	// stdout is where the command's results go.
	setup func(fs *flag.FlagSet, stdout io.Writer) func(pos []string) error
}

const (
	forceEnsureHelp = "REBUILD: discard node_modules and re-link it from the cache even if its marker already matches the current lockfile hash (manual changes like npm link are lost)"
	forceGCHelp     = "REALLY DELETE stale entries; without it (or without --roots) gc only reports what it would remove"
	rootsHelp       = "directory (repeatable or comma-separated) to scan recursively for package-lock.json files; must exist"
	jsonHelp        = "print the gc report as JSON on stdout"
	listJSONHelp    = "print the listing as JSON on stdout"
)

var commands = []command{
	{"ensure", "<dir>", func(fs *flag.FlagSet, _ io.Writer) func([]string) error {
		force := fs.Bool("force", false, forceEnsureHelp)
		root := fs.String("cache-root", "", "cache root; overrides $FLEET_CACHE_DIR")
		return func(pos []string) error { return runEnsure(pos[0], *force, *root) }
	}},
	{"gc", "", func(fs *flag.FlagSet, _ io.Writer) func([]string) error {
		var roots stringSliceFlag
		fs.Var(&roots, "roots", rootsHelp)
		force := fs.Bool("force", false, forceGCHelp)
		asJSON := fs.Bool("json", false, jsonHelp)
		root := fs.String("cache-root", "", "cache root; overrides $FLEET_CACHE_DIR")
		return func([]string) error { return runGC(roots.values, *force, *asJSON, *root) }
	}},
	{"ensure-windows", "<wsl-dir>", func(fs *flag.FlagSet, _ io.Writer) func([]string) error {
		force := fs.Bool("force", false, forceEnsureHelp)
		root := fs.String("cache-root", "", "WSL-visible path to the Windows-native cache root (repos.yaml's windows_cache_root); auto-detected via %LOCALAPPDATA% if omitted")
		return func(pos []string) error { return runEnsureWindows(pos[0], *force, *root) }
	}},
	{"gc-windows", "", func(fs *flag.FlagSet, _ io.Writer) func([]string) error {
		var roots stringSliceFlag
		fs.Var(&roots, "roots", rootsHelp)
		force := fs.Bool("force", false, forceGCHelp)
		asJSON := fs.Bool("json", false, jsonHelp)
		root := fs.String("cache-root", "", "WSL-visible path to the Windows-native cache root; auto-detected via %LOCALAPPDATA% if omitted")
		return func([]string) error { return runGCWindows(roots.values, *force, *asJSON, *root) }
	}},
	{"list", "", func(fs *flag.FlagSet, stdout io.Writer) func([]string) error {
		asJSON := fs.Bool("json", false, listJSONHelp)
		root := fs.String("cache-root", "", "cache root (any WSL-visible path, including a Windows cache root); overrides $FLEET_CACHE_DIR")
		return func([]string) error { return runList(stdout, *asJSON, *root) }
	}},
}

// parseInterspersed parses args allowing flags before or after positional
// arguments (Go's flag package alone stops at the first positional). A bare
// "--" ends flag parsing; everything after it is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos, tail []string
	for i, a := range args {
		if a == "--" {
			tail = args[i+1:]
			args = args[:i]
			break
		}
	}
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	return append(pos, tail...), nil
}

// run executes fleet-cache with args (excluding the program name) and
// returns the process exit code: cli.ExitOK, cli.ExitFailure for an
// operation that failed, cli.ExitUsage for a usage error (as fleet-task and
// fleet-run do). Error lines go to stderr as "fleet-cache: error: <msg>".
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		return usageError(stderr, "missing command")
	}
	name := args[0]
	switch name {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return cli.ExitOK
	case "--version", "version":
		if len(args) > 1 {
			return usageError(stderr, fmt.Sprintf("%s: unexpected argument %q", name, args[1]))
		}
		fmt.Fprintf(stdout, "fleet-cache %s\n", cli.Version(version))
		return cli.ExitOK
	}

	for _, c := range commands {
		if c.name != name {
			continue
		}
		fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		runFn := c.setup(fs, stdout)
		pos, err := parseInterspersed(fs, args[1:])
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			fmt.Fprintf(stdout, "\nFlags of %s:\n", c.name)
			fs.SetOutput(stdout)
			fs.PrintDefaults()
			return cli.ExitOK
		}
		if err != nil {
			return usageError(stderr, fmt.Sprintf("%s: %v", c.name, err))
		}
		want := 0
		if c.posName != "" {
			want = 1
		}
		if len(pos) != want {
			if want == 1 {
				return usageError(stderr, fmt.Sprintf("%s: expected exactly one %s argument", c.name, c.posName))
			}
			return usageError(stderr, fmt.Sprintf("%s: unexpected argument %q", c.name, pos[0]))
		}
		if err := runFn(pos); err != nil {
			cli.FErrorf(stderr, "%s: %v", c.name, err)
			return cli.ExitFailure
		}
		return cli.ExitOK
	}

	return usageError(stderr, fmt.Sprintf("unknown command %q", name))
}

// usageError prints msg as an error line, then the usage text, to stderr
// and returns cli.ExitUsage.
func usageError(stderr io.Writer, msg string) int {
	cli.FErrorf(stderr, "%s", msg)
	fmt.Fprintln(stderr)
	fmt.Fprint(stderr, usage)
	return cli.ExitUsage
}

func main() {
	cli.SetProgram("fleet-cache")
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
