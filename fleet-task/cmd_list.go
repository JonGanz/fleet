package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

func cmdList(jsonOut bool) error {
	td, err := tasksDir()
	if err != nil {
		return err
	}
	tasks, err := listTasks(td)
	if err != nil {
		return err
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tasks)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TICKET\tDESCRIPTION\tREPOS\tCREATED_AT")
	for _, t := range tasks {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", t.Ticket, t.Description, len(t.Repos), t.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	return w.Flush()
}
