package main

// Integration tests: drive the real command functions against a private tmux
// server (FLEET_TMUX_SOCKET) with temp config/state dirs. They never touch
// the user's own tmux server, ~/.config or ~/.local/state, and are skipped
// when tmux is not installed.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const itSession = "fleetit"

// itEnv is one sandboxed fleet environment.
type itEnv struct {
	socket   string
	cfgDir   string
	stateDir string
	wtRoot   string
}

// newITEnv sets up the sandbox: private tmux socket, temp config/state dirs,
// an empty tmux config (so the user's ~/.tmux.conf isn't loaded), and
// FLEET_NO_INPUT=1 so no picker can open. The tmux server is killed on
// cleanup.
func newITEnv(t *testing.T) *itEnv {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	e := &itEnv{
		socket:   fmt.Sprintf("fleet-run-it-%d-%d", os.Getpid(), time.Now().UnixNano()),
		cfgDir:   t.TempDir(),
		stateDir: t.TempDir(),
		wtRoot:   t.TempDir(),
	}
	t.Setenv(tmuxSocketEnv, e.socket)
	t.Setenv("FLEET_CONFIG_DIR", e.cfgDir)
	t.Setenv("FLEET_STATE_DIR", e.stateDir)
	t.Setenv("FLEET_REPOS_FILE", "")
	t.Setenv(noInputEnv, "1")
	t.Setenv("TMUX", "")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", e.socket, "kill-server").Run()
		// tmux leaves the socket file behind; remove it from tmux's
		// default socket dir.
		tmpDir := os.Getenv("TMUX_TMPDIR")
		if tmpDir == "" {
			tmpDir = "/tmp"
		}
		_ = os.Remove(filepath.Join(tmpDir, fmt.Sprintf("tmux-%d", os.Getuid()), e.socket))
	})

	tmuxConf := filepath.Join(e.cfgDir, "tmux.conf")
	// base-index 1 checks nothing assumes windows start at 0.
	mustWrite(t, tmuxConf, "set -g base-index 1\n", 0o644)
	repos := fmt.Sprintf(`tmux:
  session_name: %s
  config_file: %s
repos:
  - name: backend
    run:
      - name: api
        cmd: "sleep 300"
      - name: crash
        cmd: "echo boom; exit 7"
  - name: my.app
    run:
      - name: dev
        cmd: "sleep 300"
      - name: crash
        cmd: "echo dotted-boom; exit 5"
`, itSession, tmuxConf)
	mustWrite(t, filepath.Join(e.cfgDir, "repos.yaml"), repos, 0o644)
	e.writeTask(t, "A-1", "first ticket", "backend", "my.app")
	e.writeTask(t, "B-2", "second ticket", "backend")
	return e
}

// writeTask writes tasks/<ticket>.json with a real worktree dir per repo.
func (e *itEnv) writeTask(t *testing.T, ticket, desc string, repos ...string) {
	t.Helper()
	ts := TaskState{Ticket: ticket, Description: desc, CreatedAt: time.Now()}
	for _, r := range repos {
		dir := filepath.Join(e.wtRoot, ticket, r)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ts.Repos = append(ts.Repos, TaskRepo{Repo: r, Branch: "br-" + ticket, WorktreePath: dir})
	}
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.stateDir, "tasks", ticket+".json"), string(data), 0o644)
}

// tmux runs tmux against the private server and returns trimmed stdout.
func (e *itEnv) tmux(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", e.socket}, args...)...).Output()
	if err != nil {
		t.Fatalf("tmux %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// captureStdout runs f with os.Stdout redirected, returning what it printed.
func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	ferr := f()
	os.Stdout = old
	w.Close()
	return <-done, ferr
}

// runJSON runs the CLI with args and decodes its stdout into v.
func runJSON(t *testing.T, v any, args ...string) error {
	t.Helper()
	out, err := captureStdout(t, func() error { return run(args) })
	if out != "" {
		if jerr := json.Unmarshal([]byte(out), v); jerr != nil {
			t.Fatalf("%v: bad JSON %q: %v", args, out, jerr)
		}
	}
	return err
}

func itStatus(t *testing.T) Status {
	t.Helper()
	var st Status
	if err := runJSON(t, &st, "status", "--json"); err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// waitDead polls status until window's pane is dead.
func waitDead(t *testing.T, window string) PaneInfo {
	t.Helper()
	for i := 0; i < 50; i++ {
		if p, ok := findPane(itStatus(t).Panes, window); ok && !p.Alive {
			return p
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("window %s never became a dead pane", window)
	return PaneInfo{}
}

func resultStatuses(rep StartReport) map[string]string {
	m := map[string]string{}
	for _, r := range rep.Results {
		m[r.Target] = r.Status
	}
	return m
}

func TestIntegrationLifecycle(t *testing.T) {
	e := newITEnv(t)

	// The first window creates the session (new-session path): a dotted
	// name whose command exits at once must leave a dead pane.
	var rep StartReport
	if err := runJSON(t, &rep, "start", "--ticket", "A-1", "my.app:crash", "--json"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := resultStatuses(rep)["my.app:crash"]; got != startStatusStarted {
		t.Fatalf("my.app:crash = %q", got)
	}
	if p := waitDead(t, "my.app-crash"); p.ExitStatus == nil || *p.ExitStatus != 5 {
		t.Errorf("dotted crash pane = %+v", p)
	}

	// new-window path, both linux targets plus the rest.
	rep = StartReport{}
	if err := runJSON(t, &rep, "start", "--ticket", "A-1", "--all", "--json"); err != nil {
		t.Fatalf("start --all: %v", err)
	}
	want := map[string]string{
		"backend:api": startStatusStarted, "backend:crash": startStatusStarted,
		"my.app:dev": startStatusStarted, "my.app:crash": startStatusRespawned,
	}
	for k, v := range want {
		if got := resultStatuses(rep)[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if p := waitDead(t, "backend-crash"); p.ExitStatus == nil || *p.ExitStatus != 7 {
		t.Errorf("crash pane = %+v", p)
	}

	st := itStatus(t)
	if st.ActiveTicket != "A-1" || st.Description != "first ticket" || st.StaleActive || len(st.Windows) != 4 {
		t.Errorf("status = %+v", st)
	}
	for _, w := range []string{"backend-api", "backend-crash", "my.app-dev", "my.app-crash"} {
		if e.tmux(t, "show-options", "-wv", "-t", windowID(t, e, w), "remain-on-exit") != "on" {
			t.Errorf("%s lacks remain-on-exit", w)
		}
	}

	// logs, including a dotted name and a dead pane.
	var logs LogsReport
	if err := runJSON(t, &logs, "logs", "backend:crash", "--json"); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if logs.Alive || !strings.Contains(strings.Join(logs.Lines, "\n"), "boom") {
		t.Errorf("logs = %+v", logs)
	}
	logs = LogsReport{}
	if err := runJSON(t, &logs, "logs", "my.app:crash", "--json"); err != nil {
		t.Fatalf("logs dotted: %v", err)
	}
	if !strings.Contains(strings.Join(logs.Lines, "\n"), "dotted-boom") {
		t.Errorf("dotted logs = %+v", logs)
	}

	// targets
	var tr TargetsReport
	if err := runJSON(t, &tr, "targets", "--ticket", "A-1", "--json"); err != nil {
		t.Fatalf("targets: %v", err)
	}
	running := map[string]bool{}
	for _, ti := range tr.Targets {
		if ti.Target != ti.Label {
			t.Errorf("target %q != label %q", ti.Target, ti.Label)
		}
		running[ti.Target] = ti.Running
	}
	if !running["backend:api"] || !running["my.app:dev"] || running["backend:crash"] {
		t.Errorf("running = %v", running)
	}

	// start again: live left alone, dead respawned.
	rep = StartReport{}
	if err := runJSON(t, &rep, "start", "--ticket", "A-1", "backend:api", "backend:crash", "--json"); err != nil {
		t.Fatalf("restart same ticket: %v", err)
	}
	if s := resultStatuses(rep); s["backend:api"] != startStatusAlreadyRunning || s["backend:crash"] != startStatusRespawned {
		t.Errorf("rerun = %v", s)
	}

	// restart: live pane gets a new process.
	before, _ := findPane(itStatus(t).Panes, "my.app-dev")
	rep = StartReport{}
	if err := runJSON(t, &rep, "restart", "--ticket", "A-1", "my.app:dev", "--json"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if s := resultStatuses(rep)["my.app:dev"]; s != startStatusRestarted {
		t.Errorf("restart status = %q", s)
	}
	after, _ := findPane(itStatus(t).Panes, "my.app-dev")
	if !after.Alive || after.PID == before.PID {
		t.Errorf("restart did not respawn: before %+v after %+v", before, after)
	}

	// stop a dotted target by name.
	var sr StopReport
	if err := runJSON(t, &sr, "stop", "my.app:dev", "backend:nope", "--json"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(sr.Stopped) != 1 || sr.Stopped[0] != "my.app-dev" || sr.StoppedTargets[0] != "my.app:dev" ||
		len(sr.NotRunning) != 1 || sr.NotRunningTargets[0] != "" || sr.ClearedActive {
		t.Errorf("stop report = %+v", sr)
	}

	// stop --all clears the record.
	sr = StopReport{}
	if err := runJSON(t, &sr, "stop", "--all", "--ticket", "A-1", "--json"); err != nil {
		t.Fatalf("stop --all: %v", err)
	}
	if len(sr.Stopped) != 3 || !sr.ClearedActive {
		t.Errorf("stop --all report = %+v", sr)
	}
	if st := itStatus(t); st.ActiveTicket != "" || len(st.Windows) != 0 {
		t.Errorf("status after stop = %+v", st)
	}
}

// windowID resolves a window name to its id in the test session.
func windowID(t *testing.T, e *itEnv, name string) string {
	t.Helper()
	for _, line := range strings.Split(e.tmux(t, "list-windows", "-t", "="+itSession, "-F", "#{window_id}\t#{window_name}"), "\n") {
		if id, n, _ := strings.Cut(line, "\t"); n == name {
			return id
		}
	}
	t.Fatalf("no window %s", name)
	return ""
}

func TestIntegrationTicketSwitch(t *testing.T) {
	newITEnv(t)
	if err := runJSON(t, &StartReport{}, "start", "--ticket", "A-1", "backend:api", "my.app:dev", "--json"); err != nil {
		t.Fatalf("start A-1: %v", err)
	}
	var rep StartReport
	if err := runJSON(t, &rep, "start", "--ticket", "B-2", "backend:api", "--json"); err != nil {
		t.Fatalf("start B-2: %v", err)
	}
	if rep.SwitchedFrom != "A-1" || resultStatuses(rep)["backend:api"] != startStatusStarted {
		t.Errorf("switch report = %+v", rep)
	}
	st := itStatus(t)
	if st.ActiveTicket != "B-2" || st.Description != "second ticket" || len(st.Windows) != 1 || st.Windows[0] != "backend-api" {
		t.Errorf("status after switch = %+v", st)
	}
	// --ticket on stop is an assertion against the active ticket.
	if err := run([]string{"stop", "--all", "--ticket", "A-1"}); err == nil {
		t.Error("stop --ticket A-1 should fail while B-2 is active")
	}
}

// TestIntegrationStaleActive: the session is killed by hand while the server
// survives; status must flag the stale record and stop must clear it.
func TestIntegrationStaleActive(t *testing.T) {
	e := newITEnv(t)
	if err := runJSON(t, &StartReport{}, "start", "--ticket", "A-1", "backend:api", "--json"); err != nil {
		t.Fatalf("start: %v", err)
	}
	e.tmux(t, "new-session", "-d", "-s", "keepalive", "sleep 300")
	e.tmux(t, "kill-session", "-t", "="+itSession)

	st := itStatus(t)
	if st.ActiveTicket != "A-1" || !st.StaleActive {
		t.Errorf("status = %+v, want stale A-1", st)
	}
	txt, err := captureStdout(t, func() error { return run([]string{"status"}) })
	if err != nil || !strings.Contains(txt, "STALE") {
		t.Errorf("text status = %q, %v", txt, err)
	}

	var sr StopReport
	if err := runJSON(t, &sr, "stop", "--ticket", "A-1", "--json"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !sr.ClearedActive {
		t.Errorf("stop report = %+v", sr)
	}
	if st := itStatus(t); st.ActiveTicket != "" || st.StaleActive {
		t.Errorf("status after stop = %+v", st)
	}
}

func TestIntegrationUnknownTicketIsUsageError(t *testing.T) {
	newITEnv(t)
	for _, args := range [][]string{
		{"start", "--ticket", "NOPE", "--all"},
		{"restart", "--ticket", "NOPE", "--all"},
		{"targets", "--ticket", "NOPE"},
	} {
		if err := run(args); exitCodeFor(err) != exitUsage {
			t.Errorf("%v: err = %v, want exit 2", args, err)
		}
	}
}

func TestIntegrationRunHooks(t *testing.T) {
	e := newITEnv(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	for _, phase := range []string{hookPhasePreRun, hookPhasePostRun} {
		script := fmt.Sprintf("#!/bin/sh\necho \"%s $FLEET_TICKET $FLEET_REPO $FLEET_RUN_NAME $FLEET_BRANCH $FLEET_DESCRIPTION\" >> %s\n", phase, log)
		mustWrite(t, filepath.Join(e.cfgDir, "hooks", phase, "10-log"), script, 0o755)
	}
	if err := runJSON(t, &StartReport{}, "start", "--ticket", "A-1", "backend:api", "--json"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Already running: no hooks.
	if err := runJSON(t, &StartReport{}, "start", "--ticket", "A-1", "backend:api", "--json"); err != nil {
		t.Fatalf("start again: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "pre-run A-1 backend api br-A-1 first ticket\npost-run A-1 backend api br-A-1 first ticket\n"
	if string(data) != want {
		t.Errorf("hook log = %q, want %q", data, want)
	}
}

func TestIntegrationStopWithNothingRunning(t *testing.T) {
	newITEnv(t)
	// No server at all: bare stop (no terminal) still succeeds.
	var sr StopReport
	if err := runJSON(t, &sr, "stop", "--json"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if sr.ClearedActive || !sr.OK {
		t.Errorf("report = %+v", sr)
	}
}

func TestHelpAndVersionGoToStdout(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"start", "--help"}, {"restart", "-h"}, {"attach", "--help"}, {"version"}, {"--version"}} {
		out, err := captureStdout(t, func() error { return run(args) })
		if err != nil || out == "" {
			t.Errorf("%v: out=%q err=%v", args, out, err)
		}
	}
	out, _ := captureStdout(t, func() error { return run([]string{"--version"}) })
	if !strings.HasPrefix(out, "fleet-run ") {
		t.Errorf("version output = %q", out)
	}
	out, err := captureStdout(t, func() error { return run([]string{"bogus"}) })
	if out != "" || exitCodeFor(err) != exitUsage {
		t.Errorf("unknown command: out=%q err=%v", out, err)
	}
}
