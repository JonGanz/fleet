package main

import (
	"fmt"
	"os"
	"os/exec"
)

// ensureNodeCache shells out to fleet-cache to populate worktreePath's
// node_modules, per the CLI-boundary contract. For runtime: linux repos that
// is `fleet-cache ensure <dir>`; for runtime: windows repos it's
// `fleet-cache ensure-windows [--cache-root <windows_cache_root>] <dir>`,
// since the Windows-native cache lives on a separate path and is populated
// entirely by Windows-native processes. The flag must precede <dir>: Go's
// flag package stops looking for flags at the first positional argument, so
// putting <dir> first would make fleet-cache treat --cache-root and its
// value as extra positional args and reject the call. A non-zero exit is a
// warning, not a fatal error -- the caller should continue processing other
// repos.
func ensureNodeCache(repo *RepoConfig, cfg *ReposConfig, worktreePath string) {
	var args []string
	if repo.Runtime == "windows" {
		args = []string{"ensure-windows"}
		if cfg != nil && cfg.WindowsCacheRoot != "" {
			args = append(args, "--cache-root", expandHome(cfg.WindowsCacheRoot))
		}
		args = append(args, worktreePath)
	} else {
		args = []string{"ensure", worktreePath}
	}

	cmd := exec.Command("fleet-cache", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fleet-cache %v failed: %v\n", args, err)
	}
}
