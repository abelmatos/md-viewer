//go:build !windows

package main

import (
	"fmt"
	"os"
)

// run exists so the package builds and its tests run on Linux and macOS.
func run(string, bool) int {
	fmt.Fprintln(os.Stderr, "md-viewer runs on Windows only")
	return 1
}
