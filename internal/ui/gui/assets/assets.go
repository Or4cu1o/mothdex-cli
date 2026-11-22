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

// MothWingsDown is the wings-down frame of the moth sprite (16 x 12 cells, RGBA PNG with binary alpha), drawn by
// Aquarela for T-83a. It is original project-owned art (D30), not third-party, so it has no notice.
//
//go:embed moth-wings-down.png
var MothWingsDown []byte

// MothWingsUp is the wings-up frame of the moth sprite, from the same set as MothWingsDown.
//
//go:embed moth-wings-up.png
var MothWingsUp []byte

// ControllerDB is gamecontrollerdb.txt from gabomdq/SDL_GameControllerDB (zlib), byte for byte.
//
//go:embed gamecontrollerdb.txt
var ControllerDB string
