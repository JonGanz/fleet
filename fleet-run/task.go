package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TaskRepo is one repo's worktree entry inside a task state file.
type TaskRepo struct {
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
}

// TaskState is the shape of <state dir>/tasks/<ticket>.json.
type TaskState struct {
	Ticket      string     `json:"ticket"`
	Description string     `json:"description"`
	CreatedAt   string     `json:"created_at"`
	Repos       []TaskRepo `json:"repos"`
}

// parseTaskState parses a task state file's JSON content already read into
// memory. Split out from loadTaskState so tests can exercise it without
// touching disk.
func parseTaskState(data []byte) (*TaskState, error) {
	var ts TaskState
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

// loadTaskState reads and parses a single ticket's state file.
func loadTaskState(path string) (*TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task state %s: %w", path, err)
	}
	ts, err := parseTaskState(data)
	if err != nil {
		return nil, fmt.Errorf("parse task state %s: %w", path, err)
	}
	return ts, nil
}

// listTaskFiles globs <tasks dir>/*.json and returns the matching paths,
// sorted for deterministic output. Per the contract, "what tickets currently
// exist" is always derived this way -- never from a single aggregate file.
func listTaskFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// resolveTicketState figures out which ticket's task state to operate on.
//
// If ticket is non-empty, it loads that ticket's state file directly and
// errors if it doesn't exist. If ticket is empty, per fleet-run's contract
// there is no "plain checkout" path outside of worktrees to fall back to, so
// it requires there to be exactly one task file in tasksDir: zero or more
// than one is an error asking the caller to pass --ticket explicitly.
func resolveTicketState(tasksDirPath, ticket string) (*TaskState, error) {
	if ticket != "" {
		path := filepath.Join(tasksDirPath, ticket+".json")
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("no task state found for ticket %q (expected %s): %w", ticket, path, err)
		}
		return loadTaskState(path)
	}

	files, err := listTaskFiles(tasksDirPath)
	if err != nil {
		return nil, fmt.Errorf("list task files in %s: %w", tasksDirPath, err)
	}
	switch len(files) {
	case 0:
		return nil, fmt.Errorf("no tasks found in %s; pass --ticket to operate on a specific one (none exist yet, create one with fleet-task new)", tasksDirPath)
	case 1:
		return loadTaskState(files[0])
	default:
		tickets := make([]string, 0, len(files))
		for _, f := range files {
			tickets = append(tickets, strings.TrimSuffix(filepath.Base(f), ".json"))
		}
		return nil, fmt.Errorf("multiple tasks exist (%s); pass --ticket to pick one", strings.Join(tickets, ", "))
	}
}
