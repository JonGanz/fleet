package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// cmdPatches lists available patches for one repo, or for every configured
// repo when repo is empty.
func cmdPatches(repo string, jsonOut bool) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	repoNames := cfg.repoNames()
	if repo != "" {
		if cfg.findRepo(repo) == nil {
			return usageErrorf("unknown repo %q (see `fleet-task repos`)", repo)
		}
		repoNames = []string{repo}
	}

	all := []patchInfo{}
	for _, name := range repoNames {
		patches, err := listPatches(name)
		if err != nil {
			return fmt.Errorf("listing patches for %s: %w", name, err)
		}
		all = append(all, patches...)
	}
	if jsonOut {
		return printJSON(all)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tPATCH\tSUMMARY\tFILES")
	for _, p := range all {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Repo, p.Name, dashIfEmpty(p.Summary), dashIfEmpty(strings.Join(p.Files, ",")))
	}
	return w.Flush()
}
