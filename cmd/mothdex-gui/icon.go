//go:build gui

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
)

// appIcon decodes the embedded application icon (T-83a) for SDL_SetWindowIcon. A failure is not fatal: the caller opens the
// window with the default icon.
func appIcon() (*image.RGBA, error) {
	src, err := png.Decode(bytes.NewReader(assets.AppIcon))
	if err != nil {
		return nil, fmt.Errorf("app icon: %w", err)
	}
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out, nil
}
