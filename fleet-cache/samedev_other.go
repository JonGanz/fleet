//go:build !unix

package main

// sameDevice is a no-op on non-unix builds (fleet-cache's Linux path only
// runs under WSL/Linux); the per-file EXDEV handling still applies.
func sameDevice(a, b string) (bool, error) { return true, nil }
