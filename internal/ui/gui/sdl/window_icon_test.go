//go:build gui

package sdl

import (
	"errors"
	"image"
	"testing"
)

// An SDL2 older than 2.0.5 leaves the three icon functions unbound: SetWindowIcon says so with ErrNoWindowIcon, which the
// backend logs and goes on from, instead of calling a nil function (a panic).
func TestSetWindowIconWithoutTheFunctionsIsNotAPanic(t *testing.T) {
	lib := &Library{} // nothing bound, as on an SDL2 before 2.0.5
	err := lib.SetWindowIcon(1, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	if !errors.Is(err, ErrNoWindowIcon) {
		t.Fatalf("got %v, want ErrNoWindowIcon", err)
	}
}
