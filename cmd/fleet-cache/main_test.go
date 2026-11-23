package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JonGanz/fleet/internal/cli"
)

func TestMain(m *testing.M) {
	cli.SetProgram("fleet-cache")
	os.Exit(m.Run())
}

func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		args  []string
		pos   []string
		force bool
		root  string
	}{
		{[]string{"d", "--force"}, []string{"d"}, true, ""},
		{[]string{"--force", "d"}, []string{"d"}, true, ""},
		{[]string{"--cache-root", "/c", "d"}, []string{"d"}, false, "/c"},
		{[]string{"d", "--cache-root=/c", "--force"}, []string{"d"}, true, "/c"},
		{[]string{"--force", "--", "--weird"}, []string{"--weird"}, true, ""},
		{nil, nil, false, ""},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		force := fs.Bool("force", false, "")
		root := fs.String("cache-root", "", "")
		pos, err := parseInterspersed(fs, c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if !reflect.DeepEqual(pos, c.pos) && !(len(pos) == 0 && len(c.pos) == 0) {
			t.Errorf("%v: pos = %v, want %v", c.args, pos, c.pos)
		}
		if *force != c.force || *root != c.root {
			t.Errorf("%v: force=%v root=%q", c.args, *force, *root)
		}
	}
}

func TestRunUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"ensure"},
		{"ensure", "a", "b"},
		{"ensure", "--nope", "a"},
		{"gc", "extra"},
		{"list", "extra"},
		{"list", "--force"},
		{"version", "extra"},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != cli.ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, cli.ExitUsage)
		}
		if !strings.Contains(errb.String(), "Usage:") {
			t.Errorf("%v: usage not printed to stderr: %q", args, errb.String())
		}
		if out.Len() != 0 {
			t.Errorf("%v: stdout should be empty, got %q", args, out.String())
		}
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 0 || !strings.Contains(out.String(), "Usage:") || errb.Len() != 0 {
			t.Errorf("%v: code %d out %q err %q", args, code, out.String(), errb.String())
		}
	}
	for _, args := range [][]string{{"gc", "-h"}, {"list", "--help"}, {"ensure", "--help", "dir"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 0 || !strings.Contains(out.String(), "Flags of "+args[0]) || errb.Len() != 0 {
			t.Errorf("%v: code %d out %q err %q", args, code, out.String(), errb.String())
		}
	}
}

func TestRunOperationalFailureExitsOne(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"ensure", t.TempDir()}, &out, &errb); code != cli.ExitFailure {
		t.Errorf("missing lockfile: exit %d, want %d (%s)", code, cli.ExitFailure, errb.String())
	}
	if strings.Contains(errb.String(), "Usage:") {
		t.Errorf("operational failure must not print usage: %q", errb.String())
	}
	if !strings.HasPrefix(errb.String(), "fleet-cache: error: ensure: ") {
		t.Errorf("error line must be prefixed \"fleet-cache: error: ensure: \": %q", errb.String())
	}
}

func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errb.String())
		}
		if !strings.HasPrefix(out.String(), "fleet-cache ") || !strings.HasSuffix(out.String(), "\n") {
			t.Errorf("%v: stdout = %q", args, out.String())
		}
	}
}

func TestRunListEndToEnd(t *testing.T) {
	cache := t.TempDir()
	os.MkdirAll(filepath.Join(cache, "node-cache", "abc", "node_modules"), 0o755)

	var out, errb bytes.Buffer
	if code := run([]string{"list", "--json", "--cache-root", cache}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), `"name": "abc"`) {
		t.Errorf("stdout = %q", out.String())
	}
	out.Reset()
	if code := run([]string{"list", "--cache-root", cache}, &out, &errb); code != 0 || !strings.Contains(out.String(), "abc") {
		t.Errorf("text list: exit %d, stdout %q", code, out.String())
	}
}

func TestEnsureFlagAfterPositionalAndCacheRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644)
	_, hash, _ := cacheKey(dir)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	writeMarker(filepath.Join(dir, "node_modules"), hash)

	var out, errb bytes.Buffer
	code := run([]string{"ensure", dir, "--cache-root", t.TempDir()}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestGCMissingRootEndToEnd(t *testing.T) {
	cache := t.TempDir()
	entry := filepath.Join(cache, "node-cache", "abc")
	os.MkdirAll(filepath.Join(entry, "node_modules"), 0o755)

	var out, errb bytes.Buffer
	code := run([]string{"gc", "--roots", filepath.Join(cache, "typo"), "--force", "--cache-root", cache}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatal("entry was deleted despite bad root")
	}
}

func TestGCJSONGoesToStdout(t *testing.T) {
	cache := t.TempDir()
	os.MkdirAll(filepath.Join(cache, "node-cache", "abc", "node_modules"), 0o755)

	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	var out, errb bytes.Buffer
	code := run([]string{"gc", "--json", "--cache-root", cache}, &out, &errb)
	os.Stdout = orig
	w.Close()
	got, _ := io.ReadAll(r)

	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(string(got)), "{") || !strings.Contains(string(got), `"abc"`) {
		t.Errorf("stdout = %q", got)
	}
}

func TestEnsureRelinksOldStyleMarker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"lock":1}`), 0o644)
	_, key, err := cacheKey(dir)
	if err != nil {
		t.Fatal(err)
	}

	// A complete cache entry under the new key, so ensure needs no npm ci.
	cache := t.TempDir()
	entry := filepath.Join(cache, "node-cache", key)
	os.MkdirAll(filepath.Join(entry, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(entry, "node_modules", "pkg", "index.js"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(entry, entryCompleteMarker), []byte(key+"\n"), 0o644)

	// node_modules linked by an older fleet-cache: marker = bare lockfile hash.
	nm := filepath.Join(dir, "node_modules")
	os.MkdirAll(nm, 0o755)
	os.WriteFile(filepath.Join(nm, "handmade"), nil, 0o644)
	oldKey, _ := hashFile(filepath.Join(dir, "package-lock.json"))
	writeMarker(nm, oldKey)

	var out, errb bytes.Buffer
	if code := run([]string{"ensure", dir, "--cache-root", cache}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(nm, "handmade")); err == nil {
		t.Error("an old-style marker must not count as linked; node_modules should be rebuilt")
	}
	if _, err := os.Stat(filepath.Join(nm, "pkg", "index.js")); err != nil {
		t.Errorf("node_modules not relinked from the cache: %v", err)
	}
	if !alreadyLinked(nm, key) {
		t.Error("marker should now record the new key")
	}
}
