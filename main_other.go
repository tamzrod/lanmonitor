//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The monitor is a Windows GUI application; other platforms only compile the
// shared logic so it can be vetted and tested anywhere.
func main() {
	fmt.Fprintln(os.Stderr, "lanmonitor is a Windows GUI application; build with GOOS=windows GOARCH=amd64")
	os.Exit(1)
}
