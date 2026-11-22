//go:build gui

package sdl

import (
	"errors"
	"fmt"
	"testing"
)

// A failure to restrict the Windows DLL search path says so, and is not taken for a missing library.
func TestOpenDoesNotCallASearchPathFailureNotFound(t *testing.T) {
	defer func(old func() (uintptr, error)) { openLib = old }(openLib)
	cause := errors.New("Access is denied")
	openLib = func() (uintptr, error) { return 0, fmt.Errorf("%w: %w", ErrSearchPath, cause) }
	_, err := Open()
	if !errors.Is(err, ErrSearchPath) || errors.Is(err, ErrNotFound) || !errors.Is(err, cause) {
		t.Fatalf("%v", err)
	}
	openLib = func() (uintptr, error) { return 0, errors.New("no such file") }
	if _, err := Open(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a library that is not there is still not found: %v", err)
	}
}
