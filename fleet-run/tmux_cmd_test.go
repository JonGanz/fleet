package main

import "reflect"

import "testing"

func TestNewSessionArgv(t *testing.T) {
	got := newSessionArgv("fleet", "")
	want := []string{"new-session", "-d", "-s", "fleet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionArgv(no config) = %v, want %v", got, want)
	}

	got = newSessionArgv("fleet", "/home/jon/.config/fleet/tmux.conf")
	want = []string{"-f", "/home/jon/.config/fleet/tmux.conf", "new-session", "-d", "-s", "fleet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newSessionArgv(with config) = %v, want %v", got, want)
	}
}

func TestListWindowsArgs(t *testing.T) {
	got := listWindowsArgs("fleet")
	want := []string{"list-windows", "-t", "fleet", "-F", "#{window_name}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listWindowsArgs = %v, want %v", got, want)
	}
}

func TestNewWindowArgsLinux(t *testing.T) {
	got := newWindowArgsLinux("fleet", "PROJ-1234-backend-api", "/state/worktrees/PROJ-1234/backend", "npm run start:dev")
	want := []string{"new-window", "-a", "-t", "fleet", "-n", "PROJ-1234-backend-api", "-c", "/state/worktrees/PROJ-1234/backend", "npm run start:dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newWindowArgsLinux = %v, want %v", got, want)
	}
}

func TestNewWindowArgsWindows(t *testing.T) {
	got := newWindowArgsWindows("fleet", "PROJ-1234-admin-ui-dev", `C:\worktrees\PROJ-1234\admin-ui`, "npm run dev")
	want := []string{
		"new-window", "-a", "-t", "fleet", "-n", "PROJ-1234-admin-ui-dev",
		"powershell.exe", "-NoExit", "-Command",
		`cd 'C:\worktrees\PROJ-1234\admin-ui'; npm run dev`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newWindowArgsWindows = %v, want %v", got, want)
	}
}

func TestKillWindowArgs(t *testing.T) {
	got := killWindowArgs("fleet", "PROJ-1234-backend-api")
	want := []string{"kill-window", "-t", "fleet:PROJ-1234-backend-api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("killWindowArgs = %v, want %v", got, want)
	}
}

func TestHasSessionArgs(t *testing.T) {
	got := hasSessionArgs("fleet")
	want := []string{"has-session", "-t", "fleet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hasSessionArgs = %v, want %v", got, want)
	}
}
