// Package cli is the command-line plumbing shared by the fleet binaries:
// how diagnostics are printed, how errors map to exit codes, whether
// prompts may be shown, how the version string is resolved and how --json
// documents are written.
//
// Every diagnostic line on stderr has the form "<program>: error: <msg>" or
// "<program>: warning: <msg>", where <program> is set once by each binary's
// main via SetProgram. Progress output that is neither is written directly
// by the commands and is not routed through here.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
)

// Exit codes shared by every fleet binary: 0 ok, 1 the operation failed
// (fully or partly), 2 usage error or input required.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// NoInputEnv, when set to "1", disables every picker and prompt even if
// stdin is a terminal (e.g. an agent running inside a terminal emulator).
const NoInputEnv = "FLEET_NO_INPUT"

var (
	mu      sync.Mutex
	program = "fleet"
	// stderr is where Errorf and Warnf write; tests may swap it.
	stderr io.Writer = os.Stderr
)

// UsageError marks a failure caused by how the command was invoked: bad
// flags or arguments, unknown names, or a value that would need a prompt
// when no prompt may be shown. It maps to exit code 2.
type UsageError struct {
	Msg string
}

// diagnostic writes one "<program>: <kind>: <msg>" line to w (stderr when w
// is nil). A trailing newline in msg is dropped so the line is never
// followed by a blank one.
func diagnostic(w io.Writer, kind, format string, args ...any) {
	msg := strings.TrimSuffix(fmt.Sprintf(format, args...), "\n")
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		w = stderr
	}
	fmt.Fprintf(w, "%s: %s: %s\n", program, kind, msg)
}

// Errorf prints a "<program>: error: <msg>" line to stderr without aborting,
// for failures reported while the rest of the work is still attempted (and
// by Report for the error a command returns).
func Errorf(format string, args ...any) {
	diagnostic(nil, "error", format, args...)
}

// ExitCode maps an error returned by a command to the process exit code:
// ExitOK for nil or flag.ErrHelp (help was printed as asked), ExitUsage for
// a usage error, ExitFailure otherwise.
func ExitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return ExitOK
	case IsUsage(err):
		return ExitUsage
	default:
		return ExitFailure
	}
}

// FErrorf is Errorf writing to w instead of stderr, for code that is handed
// its own stderr writer.
func FErrorf(w io.Writer, format string, args ...any) {
	diagnostic(w, "error", format, args...)
}

// InteractiveAllowed reports whether pickers and prompts may be shown: stdin
// must be a real terminal (a Cygwin/MSYS pty counts; a pipe or /dev/null does
// not, which is why isatty is used rather than ModeCharDevice) and
// FLEET_NO_INPUT must not be "1". Only stdin is checked, never stdout:
// commands are legitimately run under $(...) with stdout captured while a
// human still answers the picker. The pickers render on /dev/tty, so without
// this guard an agent could open a picker on its user's terminal and hang.
func InteractiveAllowed() bool {
	fd := os.Stdin.Fd()
	return interactiveFrom(isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd), os.Getenv(NoInputEnv))
}

// interactiveFrom is InteractiveAllowed's pure decision, for testing.
func interactiveFrom(stdinIsTerminal bool, noInput string) bool {
	return stdinIsTerminal && noInput != "1"
}

// IsUsage reports whether err is (or wraps) a *UsageError.
func IsUsage(err error) bool {
	var ue *UsageError
	return errors.As(err, &ue)
}

// PrintJSON writes v to stdout as indented JSON followed by a newline.
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Program returns the name set by SetProgram ("fleet" if never set).
func Program() string {
	mu.Lock()
	defer mu.Unlock()
	return program
}

// Report prints err as an error line (unless it is nil or flag.ErrHelp) and
// returns the exit code for it; mains end with os.Exit(cli.Report(err)).
func Report(err error) int {
	code := ExitCode(err)
	if code != ExitOK {
		Errorf("%v", err)
	}
	return code
}

// resolveVersion is Version's pure core: the stamped value unless it is
// empty or "dev"; else the module version recorded by `go install
// module@vX`; else the 12-character VCS revision Go embedded at build time
// (suffixed "-dirty" for a modified tree); else "dev".
func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if !ok || info == nil {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var revision string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return revision
}

// SetProgram sets the program name that prefixes every diagnostic line
// ("fleet-task", "fleet-run", "fleet-cache"). Each main calls it first.
func SetProgram(name string) {
	mu.Lock()
	defer mu.Unlock()
	program = name
}

// SetStderr redirects diagnostics to w and returns a function restoring the
// previous writer. Meant for tests.
func SetStderr(w io.Writer) (restore func()) {
	mu.Lock()
	defer mu.Unlock()
	old := stderr
	stderr = w
	return func() {
		mu.Lock()
		defer mu.Unlock()
		stderr = old
	}
}

// Error returns the message.
func (e *UsageError) Error() string { return e.Msg }

// Usagef builds a *UsageError from a format string.
func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// Version returns the version to report for a binary whose
// -ldflags "-X main.version=<v>" value is stamped (pass "dev" or "" when it
// was not stamped). See resolveVersion for the fallbacks.
func Version(stamped string) string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(stamped, info, ok)
}

// Warnf prints a "<program>: warning: <msg>" line to stderr, for non-fatal
// problems.
func Warnf(format string, args ...any) {
	diagnostic(nil, "warning", format, args...)
}
