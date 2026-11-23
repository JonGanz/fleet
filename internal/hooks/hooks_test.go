package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
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

// TestRun runs real hook scripts: sorted order, env, output routing, and a
// failing hook warning without stopping the next one.
func TestRun(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "hooks", PreCreate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"10-fail": "#!/bin/sh\nexit 3\n",
		"20-log":  "#!/bin/sh\necho \"$FLEET_TICKET $FLEET_REPO $FLEET_BRANCH\"\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	defer cli.SetStderr(&stderr)()
	if err := Run(PreCreate, Env{Ticket: "T-1", Repo: "web", Branch: "b"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "T-1 web b" {
		t.Errorf("hook after a failing one saw/printed %q", got)
	}
	if !strings.Contains(stderr.String(), "warning: pre-create hook "+filepath.Join(dir, "10-fail")+" failed for web: exit status 3") {
		t.Errorf("failure warning = %q", stderr.String())
	}
	// No hooks dir for the phase: nothing happens.
	if err := Run(PostCreate, Env{}, &stdout); err != nil {
		t.Errorf("missing phase dir: %v", err)
	}
}
