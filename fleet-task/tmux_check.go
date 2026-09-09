package main

import (
	"bytes"
	"os/exec"
	"strings"
)

// activeFleetTicketOption is the global tmux user option fleet-run sets to
// record which ticket's windows are currently live (see fleet-run/
// activeticket.go and docs/CONTRACT.md's "tmux session & window naming").
// It's part of the documented cross-tool contract specifically so other
// tools -- and the user's own tmux status-line config -- can read it
// directly, so fleet-task reading it here isn't reaching into fleet-run's
// internals. Duplicated as a plain `tmux` shell-out rather than shared code,
// per this repo's no-shared-Go-code-between-modules design (see winpath.go
// for the same pattern).
const activeFleetTicketOption = "@fleet_task_ticket"

// activeFleetTicket reports the ticket fleet-run currently has running, if
// any. found is false if nothing is set, tmux has no running server, or the
// tmux binary itself isn't available -- all of which mean the same thing
// here: there's nothing live to worry about.
func activeFleetTicket() (ticket string, found bool) {
	cmd := exec.Command("tmux", "show-options", "-g", "-v", activeFleetTicketOption)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	value := strings.TrimSpace(out.String())
	if value == "" {
		return "", false
	}
	return value, true
}
