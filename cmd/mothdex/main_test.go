package main

import (
	"os"
	"strings"
	"testing"
)

// Regression (T-22 F3): /dev/null and NUL are character devices but not terminals, so a command run
// with stdin from os.DevNull must not prompt (it would read EOF and exit 130 instead of 2).
func TestIsTerminalRejectsTheNullDevice(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	if isTerminal(null) {
		t.Fatal("os.DevNull reported as a terminal")
	}
	if isTerminal(strings.NewReader("")) {
		t.Fatal("a non-file reported as a terminal")
	}
}
