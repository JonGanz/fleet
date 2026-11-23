package main

import (
	"reflect"
	"testing"

	"github.com/JonGanz/fleet/internal/wsl"
)

// remainOnExit is the chained suffix every window-creating argv ends with;
// target is the creation target, never the window name (names may hold ".").
func remainOnExit(target string) []string {
	return []string{";", "set-option", "-w", "-t", target, "remain-on-exit", "on"}
}

func TestNewSessionWithWindowArgsLinux(t *testing.T) {
	got := newSessionWithWindowArgsLinux("fleet", "", "PROJ-1234-backend-api", "/state/worktrees/PROJ-1234/backend", "npm run start:dev")
	want := []string{"new-session", "-d", "-s", "fleet", "-n", "PROJ-1234-backend-api", "-c", "/state/worktrees/PROJ-1234/backend", "npm run start:dev"}
	want = append(want, remainOnExit("=fleet:")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionWithWindowArgsLinux(no config) = %v, want %v", got, want)
	}

	got = newSessionWithWindowArgsLinux("fleet", "/home/jon/.config/fleet/tmux.conf", "PROJ-1234-backend-api", "/state/worktrees/PROJ-1234/backend", "npm run start:dev")
	want = []string{"-f", "/home/jon/.config/fleet/tmux.conf", "new-session", "-d", "-s", "fleet", "-n", "PROJ-1234-backend-api", "-c", "/state/worktrees/PROJ-1234/backend", "npm run start:dev"}
	want = append(want, remainOnExit("=fleet:")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionWithWindowArgsLinux(with config) = %v, want %v", got, want)
	}
}

func TestNewSessionWithWindowArgsWindows(t *testing.T) {
	got := newSessionWithWindowArgsWindows("fleet", "", "PROJ-1234-admin-ui-dev", `C:\worktrees\PROJ-1234\admin-ui`, "npm run dev")
	want := []string{
		"new-session", "-d", "-s", "fleet", "-n", "PROJ-1234-admin-ui-dev",
		"powershell.exe", "-NoExit", "-Command",
		`Set-Location -LiteralPath 'C:\worktrees\PROJ-1234\admin-ui'; npm run dev`,
	}
	want = append(want, remainOnExit("=fleet:")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionWithWindowArgsWindows(no config) = %v, want %v", got, want)
	}

	got = newSessionWithWindowArgsWindows("fleet", "/home/jon/.config/fleet/tmux.conf", "PROJ-1234-admin-ui-dev", `C:\worktrees\PROJ-1234\admin-ui`, "npm run dev")
	want = []string{
		"-f", "/home/jon/.config/fleet/tmux.conf",
		"new-session", "-d", "-s", "fleet", "-n", "PROJ-1234-admin-ui-dev",
		"powershell.exe", "-NoExit", "-Command",
		`Set-Location -LiteralPath 'C:\worktrees\PROJ-1234\admin-ui'; npm run dev`,
	}
	want = append(want, remainOnExit("=fleet:")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionWithWindowArgsWindows(with config) = %v, want %v", got, want)
	}
}

func TestListWindowsArgs(t *testing.T) {
	got := listWindowsArgs("fleet")
	want := []string{"list-windows", "-t", "=fleet", "-F", "#{window_id}\t#{window_name}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listWindowsArgs = %v, want %v", got, want)
	}
}

func TestNewWindowArgsLinux(t *testing.T) {
	got := newWindowArgsLinux("fleet", "PROJ-1234-backend-api", "/state/worktrees/PROJ-1234/backend", "npm run start:dev", 3)
	want := []string{"new-window", "-t", "=fleet:3", "-n", "PROJ-1234-backend-api", "-c", "/state/worktrees/PROJ-1234/backend", "npm run start:dev"}
	want = append(want, remainOnExit("=fleet:3")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newWindowArgsLinux = %v, want %v", got, want)
	}
}

func TestNewWindowArgsWindows(t *testing.T) {
	got := newWindowArgsWindows("fleet", "PROJ-1234-admin-ui-dev", `C:\worktrees\PROJ-1234\admin-ui`, "npm run dev", 2)
	want := []string{
		"new-window", "-t", "=fleet:2", "-n", "PROJ-1234-admin-ui-dev",
		"powershell.exe", "-NoExit", "-Command",
		`Set-Location -LiteralPath 'C:\worktrees\PROJ-1234\admin-ui'; npm run dev`,
	}
	want = append(want, remainOnExit("=fleet:2")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newWindowArgsWindows = %v, want %v", got, want)
	}
}

func TestListWindowIndicesArgs(t *testing.T) {
	got := listWindowIndicesArgs("fleet")
	want := []string{"list-windows", "-t", "=fleet", "-F", "#{window_index}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listWindowIndicesArgs = %v, want %v", got, want)
	}
}

func TestKillWindowArgs(t *testing.T) {
	got := killWindowArgs("@7")
	want := []string{"kill-window", "-t", "@7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("killWindowArgs = %v, want %v", got, want)
	}
}

func TestHasSessionArgs(t *testing.T) {
	got := hasSessionArgs("fleet")
	want := []string{"has-session", "-t", "=fleet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hasSessionArgs = %v, want %v", got, want)
	}
}

func TestSetSessionOptionArgs(t *testing.T) {
	got := setSessionOptionArgs("@fleet_task_ticket", "PROJ-1234")
	want := []string{"set-option", "-g", "@fleet_task_ticket", "PROJ-1234"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("setSessionOptionArgs = %v, want %v", got, want)
	}
}

func TestUnsetSessionOptionArgs(t *testing.T) {
	got := unsetSessionOptionArgs("@fleet_task_ticket")
	want := []string{"set-option", "-gu", "@fleet_task_ticket"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unsetSessionOptionArgs = %v, want %v", got, want)
	}
}

func TestShowSessionOptionArgs(t *testing.T) {
	got := showSessionOptionArgs("@fleet_task_ticket")
	want := []string{"show-options", "-g", "-v", "@fleet_task_ticket"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("showSessionOptionArgs = %v, want %v", got, want)
	}
}

func TestCapturePaneArgs(t *testing.T) {
	got := capturePaneArgs("@3", 200)
	want := []string{"capture-pane", "-p", "-J", "-S", "-200", "-t", "@3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("capturePaneArgs = %v, want %v", got, want)
	}
}

func TestListPanesArgs(t *testing.T) {
	got := listPanesArgs("fleet")
	want := []string{"list-panes", "-s", "-t", "=fleet", "-F", "#{window_id}\t#{window_name}\t#{pane_dead}\t#{pane_pid}\t#{pane_dead_status}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listPanesArgs = %v, want %v", got, want)
	}
}

func TestRespawnPaneArgsLinux(t *testing.T) {
	got := respawnPaneArgsLinux("@4", "/wt/backend", "npm run start:dev")
	want := []string{"respawn-pane", "-k", "-t", "@4", "-c", "/wt/backend", "npm run start:dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("respawnPaneArgsLinux = %v, want %v", got, want)
	}
}

func TestRespawnPaneArgsWindows(t *testing.T) {
	got := respawnPaneArgsWindows("@5", `C:\wt\admin-ui`, "npm run dev")
	want := []string{"respawn-pane", "-k", "-t", "@5", "powershell.exe", "-NoExit", "-Command", `Set-Location -LiteralPath 'C:\wt\admin-ui'; npm run dev`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("respawnPaneArgsWindows = %v, want %v", got, want)
	}
}

func TestAttachArgs(t *testing.T) {
	if got, want := attachSessionArgs("fleet"), []string{"attach-session", "-t", "=fleet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("attachSessionArgs = %v, want %v", got, want)
	}
	if got, want := switchClientArgs("fleet"), []string{"switch-client", "-t", "=fleet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("switchClientArgs = %v, want %v", got, want)
	}
	if got, want := selectWindowArgs("@2"), []string{"select-window", "-t", "@2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("selectWindowArgs = %v, want %v", got, want)
	}
	if got, want := socketPathArgs("fleet"), []string{"display-message", "-p", "-t", "=fleet:", "#{socket_path}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("socketPathArgs = %v, want %v", got, want)
	}
}

func TestWindowsPowershellCommandQuoting(t *testing.T) {
	got := windowsPowershellCommand(`C:\it's [here]`, "npm run dev")
	want := `Set-Location -LiteralPath 'C:\it''s [here]'; npm run dev`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := wsl.PSQuote("a'b"), "'a''b'"; got != want {
		t.Errorf("powershellQuote = %q, want %q", got, want)
	}
}
