package main

import (
	"fmt"

	"github.com/JonGanz/fleet/internal/picker"
)

// selectMulti opens the shared multiselect picker. Pressing enter with
// nothing checked confirms just the item under the cursor (fzf's behavior of
// a bare enter selecting the highlighted line). Returns (nil, error) if the
// user cancels (esc/ctrl-c). Callers check cli.InteractiveAllowed first.
func selectMulti(items []string) ([]string, error) {
	return picker.Run(picker.Options{Items: items, Multi: true, CursorFallback: true})
}

// selectOne opens the shared picker without checkboxes: enter immediately
// confirms the highlighted item.
func selectOne(items []string) (string, error) {
	selected, err := picker.Run(picker.Options{Items: items, CursorFallback: true})
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("no selection made")
	}
	return selected[0], nil
}
