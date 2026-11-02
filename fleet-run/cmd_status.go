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
}

// formatStatus renders st as indented JSON (asJSON) or human-readable text.
func formatStatus(st Status, asJSON bool) (string, error) {
	if st.Windows == nil {
		st.Windows = []string{}
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
	if st.ActiveTicket != "" {
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
		fmt.Fprintf(&sb, "  %s\n", w)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
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
	out, err := formatStatus(Status{Session: session, ActiveTicket: ticket, Description: desc, Windows: windows}, asJSON)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
