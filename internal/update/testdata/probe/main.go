// A release binary fixture. It has no network or user configuration access.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var fail string
var version = "2.0.0"

func main() {
	exe, _ := os.Executable()
	staged := strings.HasPrefix(filepath.Base(filepath.Dir(exe)), ".mothdex-update-")
	if fail == "probe8" && len(os.Args) > 1 && os.Args[1] == "--probe" {
		os.Exit(8)
	}
	if (fail == "version" && len(os.Args) > 1 && os.Args[1] == "--version") || (fail == "probe1" && len(os.Args) > 1 && os.Args[1] == "--probe") || fail == "all" || fail == "installed" && !staged {
		os.Exit(1)
	}
	fmt.Printf("{\"data\":{\"version\":\"%s\"}}\n", version)
}
