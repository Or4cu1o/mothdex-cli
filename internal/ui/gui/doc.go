// Package gui is the pure Go model of the SDL2 emulator-mode UI (docs/ux/gui.md, docs/contracts/cli/gui.md):
// screens, focus, actions, layout and the draw list. It has no SDL and no purego, so everything in it is
// tested without a display; the backend that executes the draw list is internal/ui/gui/sdl, the only package
// that imports purego.
//
// The Model is a value. Update takes a message and the time and returns the next Model and the Cmds (service
// calls) to run off the render thread; Render builds the frame for a time into a draw.List. Time is always
// passed in, never read, so timers are tested without a clock.
package gui
