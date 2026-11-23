package main

import (
	"encoding/json"
	"os"
)

// opResult is the --json document printed by commands that change tasks
// (new, edit, mv, rm). OK is false if any repo operation failed; the command
// then also exits 1.
type opResult struct {
	Ticket      string       `json:"ticket"`
	RenamedFrom string       `json:"renamed_from,omitempty"`
	OK          bool         `json:"ok"`
	Repos       []repoResult `json:"repos"`
}

// repoResult is one repo's outcome within an opResult. Action is one of
// "created", "removed", "moved" or "skipped".
type repoResult struct {
	Repo         string   `json:"repo"`
	Action       string   `json:"action"`
	OK           bool     `json:"ok"`
	Branch       string   `json:"branch,omitempty"`
	WorktreePath string   `json:"worktree_path,omitempty"`
	Patches      []string `json:"patches"`
	PatchErrors  []string `json:"patch_errors,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// allOK reports whether every repo result succeeded.
func allOK(results []repoResult) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

// printJSON writes v to stdout as indented JSON.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
