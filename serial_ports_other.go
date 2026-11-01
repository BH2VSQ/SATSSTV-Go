//go:build !windows

package main

// listSerialPorts is a no-op on platforms without COM-port PTT support.
func listSerialPorts() []string { return nil }
