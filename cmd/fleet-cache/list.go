package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/JonGanz/fleet/internal/xdg"
)

// listItem is one directory under node-cache, also list's --json shape.
type listItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Kind is "entry" (<hash>) or "staging" (<hash>.tmp-<pid>).
	Kind string `json:"kind"`
	// Complete is true for an entry carrying entryCompleteMarker; an entry
	// without it is rebuilt by the next ensure. Always false for staging.
	Complete bool `json:"complete"`
	// Abandoned marks a staging dir older than stagingMaxAge, which gc
	// would remove; younger ones may belong to a running ensure.
	Abandoned bool      `json:"abandoned"`
	Modified  time.Time `json:"modified"`
	SizeBytes int64     `json:"size_bytes"`
}

// listReport is list's result, also its --json shape.
type listReport struct {
	NodeCache string     `json:"node_cache"`
	Entries   []listItem `json:"entries"`
}

// formatSize renders n bytes for humans (binary units, one decimal).
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n) / unit
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		if value < unit || suffix == "TiB" {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
		value /= unit
	}
	return fmt.Sprintf("%d B", n) // unreachable
}

// listCache describes every directory under nodeCache, sorted by name. A
// missing nodeCache is an empty cache, not an error. Items vanishing while
// they are being inspected (a staging dir renamed into place, an entry
// removed by gc) are skipped rather than failing the whole listing.
func listCache(nodeCache string, now time.Time) (*listReport, error) {
	report := &listReport{NodeCache: nodeCache, Entries: []listItem{}}
	items, err := os.ReadDir(nodeCache)
	if errors.Is(err, fs.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", nodeCache, err)
	}

	for _, item := range items {
		if !item.IsDir() {
			continue // not something fleet-cache creates
		}
		path := filepath.Join(nodeCache, item.Name())
		info, err := item.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspecting %s: %w", path, err)
		}
		size, err := treeDiskUsage(path)
		if err != nil {
			return nil, err
		}
		it := listItem{
			Name:      item.Name(),
			Path:      path,
			Kind:      "entry",
			Modified:  info.ModTime(),
			SizeBytes: size,
		}
		if isStagingName(item.Name()) {
			it.Kind = "staging"
			it.Abandoned = now.Sub(info.ModTime()) > stagingMaxAge
		} else if it.Complete, err = isEntryComplete(path); err != nil {
			return nil, err
		}
		report.Entries = append(report.Entries, it)
	}
	return report, nil
}

// printListReport writes r to w as JSON if asJSON, else as a table with a
// total row.
func printListReport(w io.Writer, r *listReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	if len(r.Entries) == 0 {
		_, err := fmt.Fprintf(w, "no cache entries in %s\n", r.NodeCache)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tCOMPLETE\tMODIFIED\tSIZE")
	var total int64
	for _, it := range r.Entries {
		kind, complete := it.Kind, "no"
		if it.Abandoned {
			kind += " (abandoned)"
		}
		if it.Complete {
			complete = "yes"
		}
		if it.Kind == "staging" {
			complete = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.Name, kind, complete,
			it.Modified.Local().Format("2006-01-02 15:04"), formatSize(it.SizeBytes))
		total += it.SizeBytes
	}
	fmt.Fprintf(tw, "total\t\t\t\t%s\n", formatSize(total))
	return tw.Flush()
}

// runList implements `fleet-cache list [--json] [--cache-root <dir>]`.
// It only reads, through the WSL-visible path, so it serves the
// Windows-native cache too when that root is passed as --cache-root.
func runList(stdout io.Writer, asJSON bool, cacheRootFlag string) error {
	root, err := xdg.CacheDir(cacheRootFlag)
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}
	report, err := listCache(nodeCacheDir(root), time.Now())
	if err != nil {
		return err
	}
	return printListReport(stdout, report, asJSON)
}

// treeDiskUsage sums the disk usage of everything under root (symlinks are
// counted themselves, never followed). Paths vanishing mid-walk are
// ignored; any other error fails, so a listing never under-reports quietly.
func treeDiskUsage(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("measuring %s: %w", path, err)
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("measuring %s: %w", path, err)
		}
		total += diskUsage(info)
		return nil
	})
	return total, err
}
