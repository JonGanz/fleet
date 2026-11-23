package main

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
// "created", "removed", "moved" or "skipped". Adopted marks a "created"
// repo whose worktree already existed (Patches is then null: unknown).
// BranchDeleted is set for every successful "removed" repo: whether its
// local branch was deleted from the base, with BranchKeptReason saying why
// not.
type repoResult struct {
	Repo             string   `json:"repo"`
	Action           string   `json:"action"`
	OK               bool     `json:"ok"`
	Branch           string   `json:"branch,omitempty"`
	WorktreePath     string   `json:"worktree_path,omitempty"`
	Patches          []string `json:"patches"`
	PatchErrors      []string `json:"patch_errors,omitempty"`
	Adopted          bool     `json:"adopted,omitempty"`
	BranchDeleted    *bool    `json:"branch_deleted,omitempty"`
	BranchKeptReason string   `json:"branch_kept_reason,omitempty"`
	Error            string   `json:"error,omitempty"`
}
