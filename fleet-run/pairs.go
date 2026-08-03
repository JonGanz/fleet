package main

import "fmt"

// RunPair is one selectable "repo:run-name" candidate: a repo that has a
// worktree in the current task, and one of that repo's configured run
// targets.
type RunPair struct {
	Repo        string
	RunName     string
	Cmd         string
	Runtime     string // "linux" (default) or "windows"
	WorktreeDir string
}

// Label returns the "repo:run-name" string used both as the fzf candidate
// line and as the accepted positional-argument form for `fleet-run stop`.
func (p RunPair) Label() string {
	return p.Repo + ":" + p.RunName
}

// WindowName returns the tmux window name this pair would run in for the
// given ticket.
func (p RunPair) WindowName(ticket string) string {
	return windowName(ticket, p.Repo, p.RunName)
}

// availablePairs derives the list of repo:run-name pairs available for a
// task: only repos that (a) appear in the task's worktree list and (b) have
// a matching entry in repos.yaml are included, and only that repo's
// configured `run` targets are offered.
func availablePairs(cfg *ReposConfig, ts *TaskState) []RunPair {
	if cfg == nil || ts == nil {
		return nil
	}
	var pairs []RunPair
	for _, tr := range ts.Repos {
		rc := cfg.findRepo(tr.Repo)
		if rc == nil {
			continue // repo in task state has no repos.yaml entry (removed/renamed)
		}
		runtime := rc.Runtime
		if runtime == "" {
			runtime = "linux"
		}
		for _, run := range rc.Run {
			pairs = append(pairs, RunPair{
				Repo:        rc.Name,
				RunName:     run.Name,
				Cmd:         run.Cmd,
				Runtime:     runtime,
				WorktreeDir: tr.WorktreePath,
			})
		}
	}
	return pairs
}

// findPairByLabel looks up a pair by its "repo:run-name" label.
func findPairByLabel(pairs []RunPair, label string) (RunPair, bool) {
	for _, p := range pairs {
		if p.Label() == label {
			return p, true
		}
	}
	return RunPair{}, false
}

// pairNotFoundError formats a consistent error for an unrecognized
// repo:run-name argument.
func pairNotFoundError(label string) error {
	return fmt.Errorf("no such repo:run-name %q available for this task (check repos.yaml `run` entries and the task's worktrees)", label)
}
