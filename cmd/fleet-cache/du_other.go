//go:build !unix

package main

import "io/fs"

// diskUsage returns info's apparent size: allocated blocks aren't exposed
// on non-unix builds (fleet-cache's list only really runs under WSL/Linux).
func diskUsage(info fs.FileInfo) int64 {
	return info.Size()
}
