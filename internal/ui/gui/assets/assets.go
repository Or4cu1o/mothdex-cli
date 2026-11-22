// Package assets embeds the data the GUI ships with: one font and the SDL game controller database. Both
// are pinned (see PINS.md), and a test checks their SHA-256, so a change to either is a reviewed change to
// the pin and the file together (docs/contracts/cli/gui.md sections 3 and 4). Their licences are in
// THIRD_PARTY/ at the root of the repository.
package assets

import (
	_ "embed" // the files below
)

// NotoSans is Noto Sans Regular, subset to Latin and Cyrillic (SIL OFL 1.1). One weight: the GUI uses size
// and colour for emphasis, not bold.
//
//go:embed NotoSans-Regular-subset.ttf
var NotoSans []byte

// ControllerDB is gamecontrollerdb.txt from gabomdq/SDL_GameControllerDB (zlib), byte for byte.
//
//go:embed gamecontrollerdb.txt
var ControllerDB string
