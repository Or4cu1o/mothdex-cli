// A release binary fixture. It has no network or user configuration access.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var fail string

func main() {
	exe, _ := os.Executable()
	staged := strings.HasPrefix(filepath.Base(filepath.Dir(exe)), ".mothdex-update-")
	if fail == "all" || fail == "installed" && !staged {
		os.Exit(1)
	}
	fmt.Println(`{"data":{"version":"2.0.0"}}`)
}
