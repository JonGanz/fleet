package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Status is the output of `fleet-run status`.
type Status struct {
	Session      string   `json:"session"`
	ActiveTicket string   `json:"active_ticket"`
	Description  string   `json:"description"`
	Windows      []string `json:"windows"`
	// Panes carries per-window pane state; Windows is kept for
	// compatibility with earlier consumers.
	Panes []PaneInfo `json:"panes"`
	// StaleActive is true when an active ticket is recorded but no windows
	// are running: the session was killed outside fleet-run while the tmux
	// server survived. `fleet-run stop --all` clears it.
	StaleActive bool `json:"stale_active"`
}

// formatStatus renders st as indented JSON (asJSON) or human-readable text.
func formatStatus(st Status, asJSON bool) (string, error) {
	if st.Windows == nil {
		st.Windows = []string{}
	}
	if st.Panes == nil {
		st.Panes = []PaneInfo{}
	}
	if asJSON {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if st.ActiveTicket == "" && len(st.Windows) == 0 {
		return "nothing running", nil
	}
	var sb strings.Builder
	if st.StaleActive {
		fmt.Fprintf(&sb, "active ticket: %s (STALE: nothing is running; clear it with `fleet-run stop --all`)\n", st.ActiveTicket)
		if st.Description != "" {
			fmt.Fprintf(&sb, "description:   %s\n", st.Description)
		}
	} else if st.ActiveTicket != "" {
		fmt.Fprintf(&sb, "active ticket: %s\n", st.ActiveTicket)
		if st.Description != "" {
			fmt.Fprintf(&sb, "description:   %s\n", st.Description)
		}
	} else {
		sb.WriteString("active ticket: (none recorded)\n")
	}
	if len(st.Windows) == 0 {
		sb.WriteString("windows:       (none)")
		return sb.String(), nil
	}
	sb.WriteString("windows:\n")
	for _, w := range st.Windows {
		fmt.Fprintf(&sb, "  %s%s\n", w, paneStateSuffix(st.Panes, w))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// isStaleActive reports whether an active-ticket record exists with no
// windows to back it.
func isStaleActive(activeTicket string, windowCount int) bool {
	return activeTicket != "" && windowCount == 0
}

// paneStateSuffix annotates a dead window in the text status output.
func paneStateSuffix(panes []PaneInfo, window string) string {
	pane, ok := findPane(panes, window)
	if !ok || pane.Alive {
		return ""
	}
	if pane.ExitStatus != nil {
		return fmt.Sprintf("  (dead, exit %d; see `fleet-run logs`)", *pane.ExitStatus)
	}
	return "  (dead; see `fleet-run logs`)"
}

// runStatus implements `fleet-run status [--json]`. A missing tmux server is
// not an error: it just means nothing is running.
func runStatus(asJSON bool) error {
	rf, err := reposFile()
	if err != nil {
		return err
	}
	cfg, err := loadReposConfig(rf)
	if err != nil {
		return err
	}
	session, err := requireSession(cfg)
	if err != nil {
		return err
	}

	ticket, _, err := activeTicket()
	if err != nil {
		return err
	}
	desc, _, err := getSessionOption(activeDescriptionOption)
	if err != nil {
		return err
	}
	windows, err := listWindows(session)
	if err != nil {
		return err
	}
	panes, err := listPanes(session)
	if err != nil {
		return err
	}
	st := Status{
		Session:      session,
		ActiveTicket: ticket,
		Description:  desc,
		Windows:      windowNames(windows),
		Panes:        labelPanes(panes, windowLabels(cfg)),
		StaleActive:  isStaleActive(ticket, len(windows)),
	}
	out, err := formatStatus(st, asJSON)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
