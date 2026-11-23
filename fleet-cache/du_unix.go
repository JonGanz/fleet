//go:build unix

package main

import (
	"io/fs"
	"syscall"
)

// diskUsage returns the bytes info's file occupies on disk (allocated
// blocks, like du), falling back to its apparent size when the platform
// doesn't report blocks.
func diskUsage(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}
