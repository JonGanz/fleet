package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{
		0:                "0 B",
		1023:             "1023 B",
		1024:             "1.0 KiB",
		1536:             "1.5 KiB",
		5 * 1024 * 1024:  "5.0 MiB",
		3 << 30:          "3.0 GiB",
		2048 * (1 << 40): "2048.0 TiB",
	}
	for n, want := range cases {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestListCacheIsReadOnly(t *testing.T) {
	nc := filepath.Join(t.TempDir(), "node-cache")
	old := filepath.Join(nc, "ddd.tmp-2")
	os.MkdirAll(old, 0o755)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)
	if _, err := listCache(nc, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("list must not remove anything")
	}
}

func TestListCacheKindsAndSizes(t *testing.T) {
	nc := filepath.Join(t.TempDir(), "node-cache")
	complete := filepath.Join(nc, "aaa")
	os.MkdirAll(filepath.Join(complete, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(complete, "node_modules", "pkg", "index.js"), bytes.Repeat([]byte("x"), 10000), 0o644)
	os.WriteFile(filepath.Join(complete, entryCompleteMarker), []byte("aaa\n"), 0o644)
	os.Symlink("pkg/index.js", filepath.Join(complete, "node_modules", "link"))
	os.MkdirAll(filepath.Join(nc, "bbb", "node_modules"), 0o755) // no marker
	fresh := filepath.Join(nc, "ccc.tmp-1")
	old := filepath.Join(nc, "ddd.tmp-2")
	os.MkdirAll(fresh, 0o755)
	os.MkdirAll(old, 0o755)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)
	os.WriteFile(filepath.Join(nc, "stray-file"), nil, 0o644) // ignored

	r, err := listCache(nc, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range r.Entries {
		got = append(got, it.Name+":"+it.Kind)
	}
	if strings.Join(got, ",") != "aaa:entry,bbb:entry,ccc.tmp-1:staging,ddd.tmp-2:staging" {
		t.Fatalf("entries = %v", got)
	}
	byName := map[string]listItem{}
	for _, it := range r.Entries {
		byName[it.Name] = it
	}
	if !byName["aaa"].Complete || byName["bbb"].Complete {
		t.Errorf("complete flags wrong: %+v", r.Entries)
	}
	if byName["ccc.tmp-1"].Abandoned || !byName["ddd.tmp-2"].Abandoned {
		t.Errorf("abandoned flags wrong: %+v", r.Entries)
	}
	if byName["aaa"].SizeBytes < 10000 {
		t.Errorf("aaa size %d, want >= 10000", byName["aaa"].SizeBytes)
	}
	if byName["aaa"].Path != complete {
		t.Errorf("path = %q", byName["aaa"].Path)
	}

	var text bytes.Buffer
	if err := printListReport(&text, r, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "staging (abandoned)", "total"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, text.String())
		}
	}

	var js bytes.Buffer
	if err := printListReport(&js, r, true); err != nil {
		t.Fatal(err)
	}
	var back listReport
	if err := json.Unmarshal(js.Bytes(), &back); err != nil || len(back.Entries) != 4 || back.NodeCache != nc {
		t.Fatalf("json round-trip: %v %+v", err, back)
	}
}

func TestListCacheMissingDir(t *testing.T) {
	r, err := listCache(filepath.Join(t.TempDir(), "node-cache"), time.Now())
	if err != nil || len(r.Entries) != 0 {
		t.Fatalf("missing node-cache: %+v %v", r, err)
	}
	var buf bytes.Buffer
	if err := printListReport(&buf, r, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"entries": []`) {
		t.Errorf("empty entries should be [] not null: %s", buf.String())
	}
}
