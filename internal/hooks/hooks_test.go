package hooks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/proc"
)

func TestDiscoverHooks(t *testing.T) {
	dir := t.TempDir()

	mustWrite := func(name string, mode os.FileMode) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// Executables, deliberately not in sorted creation order.
	mustWrite("20-second.sh", 0o755)
	mustWrite("10-first.sh", 0o755)
	mustWrite("30-third.sh", 0o755)
	// Non-executable file: should be skipped.
	mustWrite("05-not-executable.sh", 0o644)

	// Subdirectory: should be skipped even if "executable".
	if err := os.Mkdir(filepath.Join(dir, "01-a-directory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	hooks, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := []string{
		filepath.Join(dir, "10-first.sh"),
		filepath.Join(dir, "20-second.sh"),
		filepath.Join(dir, "30-third.sh"),
	}
	if len(hooks) != len(want) {
		t.Fatalf("Discover = %v, want %v", hooks, want)
	}
	for i := range want {
		if hooks[i] != want[i] {
			t.Errorf("hooks[%d] = %q, want %q", i, hooks[i], want[i])
		}
	}
}

func TestDiscoverHooksMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	hooks, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover on missing dir should not error, got %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("Discover on missing dir = %v, want empty", hooks)
	}
}

func TestDiscoverHooksSkipsNonExecutableAndDirs(t *testing.T) {
	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"20-b": 0o755, "10-a": 0o755, "30-noexec": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "40-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "10-a"), filepath.Join(dir, "50-link")); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "10-a"), filepath.Join(dir, "20-b"), filepath.Join(dir, "50-link")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, err := Discover(filepath.Join(dir, "missing")); err != nil || got != nil {
		t.Errorf("missing dir: %v, %v", got, err)
	}
}

func TestEnvVars(t *testing.T) {
	e := Env{Ticket: "T-1", Description: "Fix it", Repo: "api", Branch: "eng/T-1", WorktreeDir: "/wt/T-1/api"}
	want := []string{"FLEET_TICKET=T-1", "FLEET_DESCRIPTION=Fix it", "FLEET_REPO=api", "FLEET_BRANCH=eng/T-1", "FLEET_WORKTREE_DIR=/wt/T-1/api"}
	if got := e.Vars(); !reflect.DeepEqual(got, want) {
		t.Errorf("Vars = %v\nwant %v", got, want)
	}
	e.RunName = "dev"
	if got := e.Vars(); !reflect.DeepEqual(got, append(want, "FLEET_RUN_NAME=dev")) {
		t.Errorf("Vars with run name = %v", got)
	}
}

// writeHooks creates the phase's hook dir under a sandboxed config dir and
// writes each script into it, executable.
func writeHooks(t *testing.T, phase string, scripts map[string]string) string {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "hooks", phase)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRunPostPhaseWarns runs real hook scripts: sorted order, env, output
// routing, and a failing post-* hook warning without stopping the next one.
func TestRunPostPhaseWarns(t *testing.T) {
	dir := writeHooks(t, PostCreate, map[string]string{
		"10-fail": "#!/bin/sh\nexit 3\n",
		"20-log":  "#!/bin/sh\necho \"$FLEET_TICKET $FLEET_REPO $FLEET_BRANCH\"\n",
	})
	var stdout, stderr bytes.Buffer
	defer cli.SetStderr(&stderr)()
	if err := Run(PostCreate, Env{Ticket: "T-1", Repo: "web", Branch: "b"}, &stdout, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "T-1 web b" {
		t.Errorf("hook after a failing one saw/printed %q", got)
	}
	if !strings.Contains(stderr.String(), "warning: post-create hook "+filepath.Join(dir, "10-fail")+" failed for web: exit status 3") {
		t.Errorf("failure warning = %q", stderr.String())
	}
	// No hooks dir for the phase: nothing happens.
	if err := Run(PreRemove, Env{}, &stdout, time.Minute); err != nil {
		t.Errorf("missing phase dir: %v", err)
	}
}

// TestRunPrePhaseVetoes: the first failing pre-* hook stops the phase and
// is returned, naming the hook and its exit status.
func TestRunPrePhaseVetoes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	dir := writeHooks(t, PreCreate, map[string]string{
		"10-ok":   "#!/bin/sh\nexit 0\n",
		"20-fail": "#!/bin/sh\nexit 4\n",
		"30-next": "#!/bin/sh\ntouch " + marker + "\n",
	})
	err := Run(PreCreate, Env{Repo: "web"}, io.Discard, time.Minute)
	if !IsVeto(err) {
		t.Fatalf("want a veto, got %v", err)
	}
	want := "pre-create hook " + filepath.Join(dir, "20-fail") + " failed: exit status 4"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if _, serr := os.Stat(marker); !os.IsNotExist(serr) {
		t.Error("a hook after the vetoing one ran")
	}
}

// TestRunTimeout: a hung hook is killed after the timeout; in a pre-* phase
// that is a veto.
func TestRunTimeout(t *testing.T) {
	writeHooks(t, PreRun, map[string]string{"10-hang": "#!/bin/sh\nsleep 60\n"})
	start := time.Now()
	err := Run(PreRun, Env{Repo: "web", RunName: "dev"}, io.Discard, 200*time.Millisecond)
	if !IsVeto(err) || !proc.IsTimeout(err) {
		t.Fatalf("want a timeout veto, got %v", err)
	}
	if !strings.Contains(err.Error(), "killed after the 200ms timeout") {
		t.Errorf("error = %q", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("Run took %s", time.Since(start))
	}
}

func TestVetoes(t *testing.T) {
	for phase, want := range map[string]bool{PreCreate: true, PreRemove: true, PreRun: true, PostCreate: false, PostRemove: false, PostRun: false} {
		if Vetoes(phase) != want {
			t.Errorf("Vetoes(%s) = %v", phase, !want)
		}
	}
}
