package main

// Pure decision logic and result shapes for `fleet-run start`, kept separate
// from cmd_start.go's tmux glue so they're unit testable.

// Start result statuses.
const (
	startStatusAlreadyRunning = "already_running"
	startStatusFailed         = "failed"
	startStatusRespawned      = "respawned"
	startStatusStarted        = "started"
)

// StartResult is one target's outcome in `fleet-run start --json`.
type StartResult struct {
	Target string `json:"target"`
	Window string `json:"window"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// StartReport is the `fleet-run start --json` output.
type StartReport struct {
	Ticket string `json:"ticket"`
	// SwitchedFrom is the previously active ticket whose windows were
	// stopped, or "" if no switch happened.
	SwitchedFrom string        `json:"switched_from"`
	OK           bool          `json:"ok"`
	Results      []StartResult `json:"results"`
}

// failedCount returns how many results failed.
func (r StartReport) failedCount() int {
	n := 0
	for _, res := range r.Results {
		if res.Status == startStatusFailed {
			n++
		}
	}
	return n
}

// existingWindowStatus decides what start does with a target whose window
// already exists: leave a live pane alone, respawn a dead one.
func existingWindowStatus(pane PaneInfo) string {
	if pane.Alive {
		return startStatusAlreadyRunning
	}
	return startStatusRespawned
}

// resolveStartTargets picks the pairs to start without a picker: every pair
// for all, else the named "repo:run-name" labels (deduplicated, in the order
// given). Returns needPicker=true when neither was given. Every error is a
// usage error, raised before anything touches tmux.
func resolveStartTargets(pairs []RunPair, all bool, labels []string) (selected []RunPair, needPicker bool, err error) {
	if all && len(labels) > 0 {
		return nil, false, usageErrorf("--all and explicit repo:run-name targets are mutually exclusive")
	}
	if all {
		return pairs, false, nil
	}
	if len(labels) == 0 {
		return nil, true, nil
	}
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		if _, _, ok := parsePairLabel(label); !ok {
			return nil, false, usageErrorf("invalid repo:run-name argument %q (expected the form \"repo:run-name\")", label)
		}
		pair, ok := findPairByLabel(pairs, label)
		if !ok {
			return nil, false, &usageError{msg: pairNotFoundError(label).Error() + "; see `fleet-run targets`"}
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		selected = append(selected, pair)
	}
	return selected, false, nil
}
