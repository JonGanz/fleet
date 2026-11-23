//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// sameDevice reports whether a and b (both must exist) live on the same
// filesystem device, i.e. whether hardlinks between them can work.
func sameDevice(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	sa, okA := ia.Sys().(*syscall.Stat_t)
	sb, okB := ib.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return false, fmt.Errorf("cannot determine device of %s or %s", a, b)
	}
	return uint64(sa.Dev) == uint64(sb.Dev), nil
}
