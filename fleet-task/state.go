package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// TaskRepo is one repo entry within a task's state file.
type TaskRepo struct {
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
}

// TaskState is the per-task state file shape: <state dir>/tasks/<ticket>.json.
type TaskState struct {
	Ticket      string     `json:"ticket"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	Repos       []TaskRepo `json:"repos"`
}

// writeTaskState marshals the task state to JSON and writes it to
// <state dir>/tasks/<ticket>.json, taking an exclusive flock on that
// specific file as a safety belt against concurrent writers (contract:
// "Writes to a given ticket's own file should still take a flock on that
// file"). Locking is advisory and implemented via an O_EXCL sibling
// lockfile rather than syscall flock, so it behaves the same across
// platforms without a cgo/unix-only dependency.
func writeTaskState(path string, st *TaskState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	unlock, err := lockFile(path)
	if err != nil {
		return err
	}
	defer unlock()

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task state: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// readTaskState reads and parses a task state file.
func readTaskState(path string) (*TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st TaskState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse task state %s: %w", path, err)
	}
	return &st, nil
}

// listTaskFiles globs <dir>/*.json and returns the matching paths, sorted
// for deterministic output.
func listTaskFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// listTasks globs <dir>/*.json, parses each, and returns the resulting
// TaskState values sorted by ticket. Files that fail to parse are skipped
// with a warning on stderr rather than aborting the whole listing.
func listTasks(dir string) ([]*TaskState, error) {
	files, err := listTaskFiles(dir)
	if err != nil {
		return nil, err
	}
	tasks := make([]*TaskState, 0, len(files))
	for _, f := range files {
		st, err := readTaskState(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", f, err)
			continue
		}
		tasks = append(tasks, st)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Ticket < tasks[j].Ticket })
	return tasks, nil
}
