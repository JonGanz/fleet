package main

import (
	"fmt"
	"os"
	"text/tabwriter"
)

// TargetInfo is one row of `fleet-run targets`.
type TargetInfo struct {
	Label   string `json:"label"`
	Window  string `json:"window"`
	Cwd     string `json:"cwd"`
	Cmd     string `json:"cmd"`
	Runtime string `json:"runtime"`
	// Running is true only when this ticket is the active one and the
	// window has a live pane. Window names don't encode the ticket, so a
	// same-named window of another ticket never counts.
	Running bool `json:"running"`
}

// TargetsReport is the `fleet-run targets --json` output.
type TargetsReport struct {
	Ticket  string       `json:"ticket"`
	Targets []TargetInfo `json:"targets"`
}

// buildTargets describes pairs, marking which are running. panes only count
// when ticketActive (the ticket being described is the active one).
func buildTargets(pairs []RunPair, panes []PaneInfo, ticketActive bool) []TargetInfo {
	out := make([]TargetInfo, 0, len(pairs))
	for _, p := range pairs {
		info := TargetInfo{
			Label:   p.Label(),
			Window:  p.WindowName(),
			Cwd:     p.WorktreeDir,
			Cmd:     p.Cmd,
			Runtime: p.Runtime,
		}
		if pane, ok := findPane(panes, info.Window); ok && ticketActive {
			info.Running = pane.Alive
		}
		out = append(out, info)
	}
	return out
}

// runTargets implements `fleet-run targets [--ticket <id>] [--json]`.
func runTargets(ticket string, asJSON bool) error {
	cfg, ts, err := loadStartContext(ticket)
	if err != nil {
		return err
	}
	session, err := requireSession(cfg)
	if err != nil {
		return err
	}
	active, hasActive, err := activeTicket()
	if err != nil {
		return err
	}
	panes, err := listPanes(session)
	if err != nil {
		return err
	}
	report := TargetsReport{
		Ticket:  ts.Ticket,
		Targets: buildTargets(availablePairs(cfg, ts), panes, hasActive && active == ts.Ticket),
	}
	if asJSON {
		return printJSON(report)
	}
	if len(report.Targets) == 0 {
		fmt.Printf("no targets for ticket %s (check repos.yaml `run` entries)\n", ts.Ticket)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tWINDOW\tRUNTIME\tRUNNING\tCMD")
	for _, t := range report.Targets {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\n", t.Label, t.Window, t.Runtime, t.Running, t.Cmd)
	}
	return w.Flush()
}
