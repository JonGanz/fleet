package main

import (
	"fmt"
	"os"
	"os/exec"
)

// ensureNodeCache shells out to `fleet-cache ensure <worktreePath>` per the
// CLI-boundary contract. A non-zero exit is a warning, not a fatal error --
// the caller should continue processing other repos.
func ensureNodeCache(worktreePath string) {
	cmd := exec.Command("fleet-cache", "ensure", worktreePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fleet-cache ensure %s failed: %v\n", worktreePath, err)
	}
}
