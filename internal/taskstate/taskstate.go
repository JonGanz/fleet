// Package taskstate is the per-ticket state file shared by fleet-task
// (which writes it) and fleet-run (which reads it):
// <state dir>/tasks/<ticket>.json, one file per ticket (see
// docs/CONTRACT.md).
//
// The filename is authoritative for the ticket id: Read and List set
// State.Ticket from it, warning when the file's own `ticket` field
// disagrees. Every write records schema_version (SchemaVersion); a file
// written by a newer fleet with a higher schema_version is refused rather
// than misread.
package taskstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/JonGanz/fleet/internal/cli"
)

// SchemaVersion is the state file schema version this build reads and
// writes. A file without schema_version is version 1.
const SchemaVersion = 1

// Repo is one repo entry within a task's state file.
type Repo struct {
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
	// Patches lists the patch filenames applied when the worktree was
	// created. nil (JSON null) means unknown: the task predates this field.
	Patches []string `json:"patches"`
}

// State is the per-task state file shape: <state dir>/tasks/<ticket>.json.
// It carries no schema version itself, so commands can embed it in their
// --json output unchanged; the version exists only on disk (see file).
type State struct {
	Ticket      string    `json:"ticket"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Repos       []Repo    `json:"repos"`
}

// file is the on-disk form of a State: the State's fields plus
// schema_version, which comes first in the written JSON.
type file struct {
	SchemaVersion int `json:"schema_version"`
	*State
}

// TooNewError is returned for a state file whose schema_version is greater
// than SchemaVersion: it was written by a newer fleet and must not be
// misread by this one.
type TooNewError struct {
	Path    string
	Version int
}

// List reads every state file in dir (see Read) and returns them sorted by
// ticket. A file that can't be read -- unparsable, or written by a newer
// fleet -- is skipped with a warning rather than aborting the listing.
func List(dir string) ([]*State, error) {
	files, err := ListFiles(dir)
	if err != nil {
		return nil, err
	}
	tasks := make([]*State, 0, len(files))
	for _, f := range files {
		st, err := Read(f)
		if err != nil {
			cli.Warnf("skipping %s: %v", f, err)
			continue
		}
		tasks = append(tasks, st)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Ticket < tasks[j].Ticket })
	return tasks, nil
}

// ListFiles globs <dir>/*.json and returns the matching paths, sorted for
// deterministic output. "What tickets exist" is always derived this way,
// never from a single aggregate file.
func ListFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// Parse parses a state file's JSON content already read into memory. A
// missing schema_version is read as 1; a newer one is a *TooNewError (with
// an empty Path). The ticket field is taken as is; Read applies the
// filename rule.
func Parse(data []byte) (*State, error) {
	f := file{State: &State{}}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.SchemaVersion < 0 {
		return nil, fmt.Errorf("invalid schema_version %d", f.SchemaVersion)
	}
	if f.SchemaVersion == 0 {
		f.SchemaVersion = 1
	}
	if f.SchemaVersion > SchemaVersion {
		return nil, &TooNewError{Version: f.SchemaVersion}
	}
	return f.State, nil
}

// Path returns <dir>/<ticket>.json after validating the ticket id (see
// ValidateTicket), so an id from user input can never escape dir.
func Path(dir, ticket string) (string, error) {
	if err := ValidateTicket(ticket); err != nil {
		return "", err
	}
	return filepath.Join(dir, ticket+".json"), nil
}

// Read reads and parses the state file at path. The ticket id is taken from
// the filename (<ticket>.json); a `ticket` field that disagrees is warned
// about and ignored. Errors other than a missing file name the path; a
// missing file returns an error satisfying errors.Is(err, fs.ErrNotExist).
func Read(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	st, err := Parse(data)
	if err != nil {
		var tooNew *TooNewError
		if errors.As(err, &tooNew) {
			tooNew.Path = path
			return nil, tooNew
		}
		return nil, fmt.Errorf("parse task state %s: %w", path, err)
	}
	if stem := TicketFromPath(path); st.Ticket != stem {
		cli.Warnf("%s says ticket %q; using %q from its filename", path, st.Ticket, stem)
		st.Ticket = stem
	}
	return st, nil
}

// TicketFromPath returns the ticket id a state file path names: its base
// name without the .json suffix.
func TicketFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

// Error explains the mismatch and the remedy.
func (e *TooNewError) Error() string {
	return fmt.Sprintf("%s has schema_version %d, but this fleet only understands up to %d; upgrade fleet to use it", e.Path, e.Version, SchemaVersion)
}

// ValidateTicket rejects ticket ids that can't safely become both a JSON
// filename (<tasks dir>/<ticket>.json) and a worktree directory component
// (<worktree_root>/<ticket>/<repo>) -- on Linux and, for runtime: windows
// repos, on NTFS -- or that would be misread as a flag on a later command
// line. Invalid ids are usage errors (exit 2).
func ValidateTicket(ticket string) error {
	const maxTicketLen = 100
	switch {
	case ticket == "":
		return cli.Usagef("ticket id is required")
	case len(ticket) > maxTicketLen:
		return cli.Usagef("ticket id %q is longer than %d bytes", ticket, maxTicketLen)
	case strings.ContainsAny(ticket, `/\`):
		return cli.Usagef("ticket id %q must not contain path separators", ticket)
	case ticket == "." || ticket == "..":
		return cli.Usagef("ticket id %q is not valid", ticket)
	case strings.HasPrefix(ticket, "-"):
		return cli.Usagef("ticket id %q must not start with '-'", ticket)
	case strings.ContainsAny(ticket, `<>:"|?*`):
		return cli.Usagef("ticket id %q must not contain any of <>:\"|?* (not allowed in Windows filenames)", ticket)
	case strings.HasSuffix(ticket, ".") || strings.HasSuffix(ticket, " "):
		return cli.Usagef("ticket id %q must not end with '.' or a space (not allowed in Windows filenames)", ticket)
	}
	for _, r := range ticket {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError {
			return cli.Usagef("ticket id %q must not contain whitespace, control characters or invalid UTF-8", ticket)
		}
	}
	return nil
}

// Write marshals st to JSON, stamped with SchemaVersion, and writes it to
// path via a temp file + rename, so readers never see a half-written file.
// fleet-task callers must hold the ticket's lock for their whole
// read-modify-write cycle; taking a lock here, around the write alone,
// would not stop two commands from losing each other's updates.
func Write(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(file{SchemaVersion: SchemaVersion, State: st}, "", "  ")
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
