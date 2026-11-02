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
)

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

func TestRunUsageErrorsExitOne(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"ensure"},
		{"ensure", "a", "b"},
		{"ensure", "--nope", "a"},
		{"gc", "extra"},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
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
	var out, errb bytes.Buffer
	if code := run([]string{"--help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("help: code %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"gc", "-h"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "-force") {
		t.Errorf("gc -h: code %d", code)
	}
}

func TestEnsureFlagAfterPositionalAndCacheRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644)
	hash, _ := hashFile(filepath.Join(dir, "package-lock.json"))
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
