package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/JonGanz/fleet/internal/cli"
	"github.com/JonGanz/fleet/internal/config"
)

// repoInfo is one entry of `fleet-task repos --json`.
type repoInfo struct {
	Name           string      `json:"name"`
	Runtime        string      `json:"runtime"`
	DefaultBranch  string      `json:"default_branch"`
	BranchTemplate string      `json:"branch_template"`
	Run            []runInfo   `json:"run"`
	Patches        []patchInfo `json:"patches"`
}

// runInfo is one run target of a repo, as fleet-run would name it.
type runInfo struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Cmd    string `json:"cmd"`
}

// buildRepoInfos describes every configured repo, including its run
// targets and available patches.
func buildRepoInfos(cfg *config.ReposConfig) ([]repoInfo, error) {
	infos := make([]repoInfo, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		patches, err := listPatches(r.Name)
		if err != nil {
			return nil, fmt.Errorf("listing patches for %s: %w", r.Name, err)
		}
		runs := make([]runInfo, 0, len(r.Run))
		for _, run := range r.Run {
			runs = append(runs, runInfo{Name: run.Name, Target: r.Name + ":" + run.Name, Cmd: run.Cmd})
		}
		runtime := r.Runtime
		if runtime == "" {
			runtime = "linux"
		}
		infos = append(infos, repoInfo{
			Name:           r.Name,
			Runtime:        runtime,
			DefaultBranch:  r.DefaultBranch,
			BranchTemplate: r.BranchTemplate,
			Run:            runs,
			Patches:        patches,
		})
	}
	return infos, nil
}

// cmdRepos lists the repos configured in repos.yaml.
func cmdRepos(jsonOut bool) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	infos, err := buildRepoInfos(cfg)
	if err != nil {
		return err
	}
	if jsonOut {
		return cli.PrintJSON(infos)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tRUNTIME\tDEFAULT_BRANCH\tRUN\tPATCHES")
	for _, info := range infos {
		runNames := make([]string, len(info.Run))
		for i, run := range info.Run {
			runNames[i] = run.Name
		}
		patchNames := make([]string, len(info.Patches))
		for i, p := range info.Patches {
			patchNames[i] = p.Name
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Runtime, info.DefaultBranch,
			dashIfEmpty(strings.Join(runNames, ",")), dashIfEmpty(strings.Join(patchNames, ",")))
	}
	return w.Flush()
}

// dashIfEmpty returns "-" for an empty table cell.
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
