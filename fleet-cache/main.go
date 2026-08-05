package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const usage = `fleet-cache: node_modules hardlink cache keyed by package-lock.json hash

Usage:
  fleet-cache ensure <dir>
      Ensure <dir>/node_modules is populated by hardlinking from the cache,
      running "npm ci" once per unique package-lock.json hash and reusing it
      across every worktree sharing that lockfile.

  fleet-cache gc [--roots <dir>[,<dir>...]] [--force]
      Scan the cache for entries whose package-lock.json hash is no longer
      referenced by any package-lock.json under --roots (searched
      recursively, may be repeated or comma-separated), and remove them.

      Without --roots, nothing is considered referenced; entries are only
      *reported* as removable unless --force is also given, so gc is never
      destructive by default.

      Cache root: $FLEET_CACHE_DIR, else $XDG_CACHE_HOME/fleet, else
      ~/.cache/fleet. Entries live at <cache root>/node-cache/<sha256>.

  fleet-cache ensure-windows <wsl-dir> [--cache-root <wsl-path>]
      Windows-native counterpart of ensure, for runtime: windows repos
      (<wsl-dir> is the worktree's WSL-visible path, e.g. under /mnt/c/...).
      npm ci and the node_modules hardlink population both run as native
      Windows processes (PowerShell/npm), never WSL reaching across the
      9P boundary for bulk work.

      Cache root: --cache-root if given (repos.yaml's windows_cache_root),
      else auto-detected as %LOCALAPPDATA%\fleet via cmd.exe. Entries live
      at <cache root>\node-cache\<sha256>.

  fleet-cache gc-windows [--roots <dir>[,<dir>...]] [--force] [--cache-root <wsl-path>]
      Windows-native counterpart of gc; same --roots/--force safety rule,
      scanning/deleting against the Windows-native cache root instead.
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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "ensure":
		fs := flag.NewFlagSet("ensure", flag.ExitOnError)
		fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "fleet-cache ensure: expected exactly one <dir> argument")
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		if err := runEnsure(fs.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "fleet-cache ensure: %v\n", err)
			os.Exit(1)
		}

	case "gc":
		fs := flag.NewFlagSet("gc", flag.ExitOnError)
		fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
		var roots stringSliceFlag
		fs.Var(&roots, "roots", "directory (repeatable or comma-separated) to scan recursively for package-lock.json files")
		force := fs.Bool("force", false, "actually delete stale cache entries instead of just reporting them")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if err := runGC(roots.values, *force); err != nil {
			fmt.Fprintf(os.Stderr, "fleet-cache gc: %v\n", err)
			os.Exit(1)
		}

	case "ensure-windows":
		fs := flag.NewFlagSet("ensure-windows", flag.ExitOnError)
		fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
		cacheRoot := fs.String("cache-root", "", "WSL-visible path to the Windows-native cache root (repos.yaml's windows_cache_root); auto-detected via %LOCALAPPDATA% if omitted")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "fleet-cache ensure-windows: expected exactly one <wsl-dir> argument")
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		if err := runEnsureWindows(fs.Arg(0), *cacheRoot); err != nil {
			fmt.Fprintf(os.Stderr, "fleet-cache ensure-windows: %v\n", err)
			os.Exit(1)
		}

	case "gc-windows":
		fs := flag.NewFlagSet("gc-windows", flag.ExitOnError)
		fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
		var roots stringSliceFlag
		fs.Var(&roots, "roots", "directory (repeatable or comma-separated) to scan recursively for package-lock.json files")
		force := fs.Bool("force", false, "actually delete stale cache entries instead of just reporting them")
		cacheRoot := fs.String("cache-root", "", "WSL-visible path to the Windows-native cache root; auto-detected via %LOCALAPPDATA% if omitted")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if err := runGCWindows(roots.values, *force, *cacheRoot); err != nil {
			fmt.Fprintf(os.Stderr, "fleet-cache gc-windows: %v\n", err)
			os.Exit(1)
		}

	case "-h", "--help", "help":
		fmt.Print(usage)

	default:
		fmt.Fprintf(os.Stderr, "fleet-cache: unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
