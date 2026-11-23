//go:build !unix

package proc

import (
	"os"
	"os/exec"
)

// forwardSignals is a no-op where there are no process groups to forward to.
func forwardSignals(int) (stop func()) { return func() {} }

// kill kills p itself; process groups are a unix notion.
func kill(p *os.Process, _ bool) { _ = p.Kill() }

// setNewGroup is a no-op where process groups don't exist.
func setNewGroup(*exec.Cmd) {}
