package main

// windowName builds the tmux window name for a given repo/run-name pair, per
// the contract's naming convention: "<repo>-<run-name>". Only one ticket's
// windows are ever live in the session at a time (see activeticket.go), so
// the name no longer needs to encode which ticket it belongs to -- that's
// tracked separately via the session's @fleet_task_ticket option.
func windowName(repo, runName string) string {
	return repo + "-" + runName
}
