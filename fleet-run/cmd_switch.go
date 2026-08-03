package main

import "fmt"

// runSwitch implements `fleet-run switch --to <ticket>`: stop every window
// currently running in the fixed session (since only one ticket's app set
// is meant to be live at a time, per GOAL.md's "switch which set of
// applications is running"), then run the same selection+start flow as
// `start --ticket <to>`.
//
// This calls killAllWindowsInSession and startFlow directly rather than
// shelling out to its own binary, per spec.
func runSwitch(to string) error {
	if to == "" {
		return fmt.Errorf("switch requires --to <ticket>")
	}

	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}
	session := cfg.Tmux.SessionName
	if session == "" {
		return fmt.Errorf("repos.yaml tmux.session_name is required")
	}

	if err := killAllWindowsInSession(session); err != nil {
		return err
	}

	cfg, ts, err := loadStartContext(to)
	if err != nil {
		return err
	}
	return startFlow(cfg, ts)
}
