package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

const formatPatch = `From 1234abcd Mon Sep 17 00:00:00 2001
From: Dev <dev@example.com>
Date: Mon, 2 Nov 2026 10:00:00 +0000
Subject: [PATCH 1/2] Point API at local mock server

---
 src/config.ts | 2 +-
 1 file changed

diff --git a/src/config.ts b/src/config.ts
--- a/src/config.ts
+++ b/src/config.ts
@@ -1 +1 @@
-x
+y
diff --git a/old.txt b/renamed.txt
similarity index 100%
`

const plainGitDiff = `Disable telemetry for local dev

diff --git a/.env b/.env
--- a/.env
+++ b/.env
@@ -1 +1 @@
-A=1
+A=0
`

const unifiedNoGit = `--- a/one.txt	2026-01-01 00:00:00
+++ b/one.txt	2026-01-01 00:00:01
@@ -1 +1 @@
-a
+b
--- a/gone.txt
+++ /dev/null
`

func TestListPatches(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("FLEET_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "patches", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.patch"), []byte(plainGitDiff), 0o644)
	os.WriteFile(filepath.Join(dir, "a.patch"), []byte(formatPatch), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644)

	got, err := listPatches("api")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a.patch" || got[1].Name != "b.patch" {
		t.Fatalf("listPatches = %+v, want a.patch then b.patch", got)
	}
	if got[1].Summary != "Disable telemetry for local dev" || got[1].Repo != "api" {
		t.Errorf("b.patch info = %+v", got[1])
	}

	none, err := listPatches("no-such-repo")
	if err != nil || len(none) != 0 {
		t.Errorf("missing patches dir: got %v, %v; want empty, nil", none, err)
	}
}

func TestParsePatchFiles(t *testing.T) {
	cases := map[string][]string{
		formatPatch:  {"src/config.ts", "renamed.txt"},
		plainGitDiff: {".env"},
		unifiedNoGit: {"one.txt"},
		"":           {},
	}
	for content, want := range cases {
		if got := parsePatchFiles(content); !reflect.DeepEqual(got, want) {
			t.Errorf("parsePatchFiles(%.30q) = %v, want %v", content, got, want)
		}
	}
}

func TestParsePatchSummary(t *testing.T) {
	cases := map[string]string{
		formatPatch:                 "Point API at local mock server",
		plainGitDiff:                "Disable telemetry for local dev",
		unifiedNoGit:                "",
		"Subject: No prefix\n":      "No prefix",
		"Subject: [PATCH] Tagged\n": "Tagged",
	}
	for content, want := range cases {
		if got := parsePatchSummary(content); got != want {
			t.Errorf("parsePatchSummary(%.30q) = %q, want %q", content, got, want)
		}
	}
}

func TestResolvePatches(t *testing.T) {
	available := map[string][]string{
		"api": {"/p/api/a.patch", "/p/api/b.patch"},
		"ui":  {"/p/ui/c.patch"},
	}
	repos := []string{"api", "ui", "db"}

	got, err := resolvePatches(repos, available, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"api": available["api"], "ui": available["ui"], "db": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("default = %v, want all patches", got)
	}

	got, err = resolvePatches(repos, available, []string{"api:a.patch"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["api"], []string{"/p/api/b.patch"}) {
		t.Errorf("skip: api = %v", got["api"])
	}

	got, err = resolvePatches(repos, available, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for repo, ps := range got {
		if len(ps) != 0 {
			t.Errorf("--no-patches: %s = %v", repo, ps)
		}
	}

	for _, bad := range []string{"api", "api:", ":a.patch", "api:zzz.patch", "other:a.patch"} {
		if _, err := resolvePatches(repos, available, []string{bad}, false); !cli.IsUsage(err) {
			t.Errorf("skip %q: err = %v, want usage error", bad, err)
		}
	}
}
