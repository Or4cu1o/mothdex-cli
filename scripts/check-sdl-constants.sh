#!/bin/sh
# The constants and struct layouts of the SDL2 binding were written from memory (internal/ui/gui/sdl/abi.go):
# compare them with the real SDL2 headers (docs/contracts/cli/gui.md section 2). A C program is compiled
# against the headers, because enum members such as SDL_CONTROLLERBUTTONDOWN have no literal in SDL_events.h
# (they follow the previous member), so grepping the header would not work.
#
#   scripts/check-sdl-constants.sh
#
# Environment:
#   SDL_GO_PKG    the Go package to read (default internal/ui/gui/sdl; the tests of this script use copies)
#   SDL_INCLUDE   the folder that holds SDL.h (default: from pkg-config sdl2, or sdl2-config, i.e. libsdl2-dev)
#
# Exit codes: 0 all equal, 1 a mismatch or a Go constant without a mapping below, 2 the check cannot run.
set -eu
cd "$(dirname "$0")/.."

PKG=${SDL_GO_PKG:-internal/ui/gui/sdl}

# Go constant -> C expression. A new numeric constant in the package must be added here, or the check fails.
MAP='InitVideo SDL_INIT_VIDEO
InitGameController SDL_INIT_GAMECONTROLLER
WindowHidden SDL_WINDOW_HIDDEN
WindowFullscreenDesktop SDL_WINDOW_FULLSCREEN_DESKTOP
RendererSoftware SDL_RENDERER_SOFTWARE
RendererPresentVSync SDL_RENDERER_PRESENTVSYNC
TextureAccessStatic SDL_TEXTUREACCESS_STATIC
BlendNone SDL_BLENDMODE_NONE
BlendBlend SDL_BLENDMODE_BLEND
PixelFormatARGB8888 SDL_PIXELFORMAT_ARGB8888
EventQuit SDL_QUIT
EventWindow SDL_WINDOWEVENT
EventKeyDown SDL_KEYDOWN
EventKeyUp SDL_KEYUP
EventTextInput SDL_TEXTINPUT
EventControllerAxisMotion SDL_CONTROLLERAXISMOTION
EventControllerButtonDown SDL_CONTROLLERBUTTONDOWN
EventControllerButtonUp SDL_CONTROLLERBUTTONUP
EventControllerDeviceAdded SDL_CONTROLLERDEVICEADDED
EventControllerDeviceRemoved SDL_CONTROLLERDEVICEREMOVED
WindowEventResized SDL_WINDOWEVENT_RESIZED
WindowEventSizeChanged SDL_WINDOWEVENT_SIZE_CHANGED
ControllerButtonA SDL_CONTROLLER_BUTTON_A
ControllerButtonB SDL_CONTROLLER_BUTTON_B
ControllerButtonX SDL_CONTROLLER_BUTTON_X
ControllerButtonY SDL_CONTROLLER_BUTTON_Y
ControllerButtonBack SDL_CONTROLLER_BUTTON_BACK
ControllerButtonStart SDL_CONTROLLER_BUTTON_START
ControllerButtonLeftShoulder SDL_CONTROLLER_BUTTON_LEFTSHOULDER
ControllerButtonRightShoulder SDL_CONTROLLER_BUTTON_RIGHTSHOULDER
ControllerButtonDPadUp SDL_CONTROLLER_BUTTON_DPAD_UP
ControllerButtonDPadDown SDL_CONTROLLER_BUTTON_DPAD_DOWN
ControllerButtonDPadLeft SDL_CONTROLLER_BUTTON_DPAD_LEFT
ControllerButtonDPadRight SDL_CONTROLLER_BUTTON_DPAD_RIGHT
ControllerAxisLeftX SDL_CONTROLLER_AXIS_LEFTX
ControllerAxisLeftY SDL_CONTROLLER_AXIS_LEFTY
KeycodeBackspace SDLK_BACKSPACE
KeycodeTab SDLK_TAB
KeycodeReturn SDLK_RETURN
KeycodeEscape SDLK_ESCAPE
KeycodeRight SDLK_RIGHT
KeycodeLeft SDLK_LEFT
KeycodeDown SDLK_DOWN
KeycodeUp SDLK_UP
KeycodePageUp SDLK_PAGEUP
KeycodePageDown SDLK_PAGEDOWN
KeycodeKPEnter SDLK_KP_ENTER'

command -v cc >/dev/null 2>&1 || { echo "check-sdl-constants: no C compiler (cc)" >&2; exit 2; }

cflags=""
if [ -n "${SDL_INCLUDE:-}" ]; then
	cflags="-I$SDL_INCLUDE"
elif command -v pkg-config >/dev/null 2>&1 && pkg-config --exists sdl2; then
	cflags=$(pkg-config --cflags sdl2)
elif command -v sdl2-config >/dev/null 2>&1; then
	cflags=$(sdl2-config --cflags)
else
	echo "check-sdl-constants: the SDL2 headers were not found (install libsdl2-dev or set SDL_INCLUDE)" >&2
	exit 2
fi

tmp=$(mktemp -d)
trap 'rm -f "$tmp/c.c" "$tmp/c" "$tmp/c.out" "$tmp/c.err" "$tmp/go.out"; rmdir "$tmp"' EXIT

# 1. What the headers say.
{
	printf '#include <stddef.h>\n#include <stdio.h>\n#include <SDL.h>\n'
	# Struct layouts, the numbers abi_test.go relies on: a wrong one stops the compilation.
	cat <<'EOF'
_Static_assert(sizeof(SDL_version) == 3, "sizeof(SDL_version)");
_Static_assert(sizeof(SDL_Rect) == 16, "sizeof(SDL_Rect)");
_Static_assert(offsetof(SDL_Rect, x) == 0 && offsetof(SDL_Rect, y) == 4, "SDL_Rect x y");
_Static_assert(offsetof(SDL_Rect, w) == 8 && offsetof(SDL_Rect, h) == 12, "SDL_Rect w h");
_Static_assert(sizeof(SDL_Event) == 56, "sizeof(SDL_Event)");
_Static_assert(sizeof(SDL_ControllerButtonEvent) == 16, "sizeof(SDL_ControllerButtonEvent)");
_Static_assert(offsetof(SDL_ControllerButtonEvent, type) == 0, "cbutton.type");
_Static_assert(offsetof(SDL_ControllerButtonEvent, timestamp) == 4, "cbutton.timestamp");
_Static_assert(offsetof(SDL_ControllerButtonEvent, which) == 8, "cbutton.which");
_Static_assert(offsetof(SDL_ControllerButtonEvent, button) == 12, "cbutton.button");
_Static_assert(offsetof(SDL_ControllerButtonEvent, state) == 13, "cbutton.state");
_Static_assert(sizeof(SDL_WindowEvent) == 24, "sizeof(SDL_WindowEvent)");
_Static_assert(offsetof(SDL_WindowEvent, windowID) == 8, "window.windowID");
_Static_assert(offsetof(SDL_WindowEvent, event) == 12, "window.event");
_Static_assert(offsetof(SDL_WindowEvent, data1) == 16, "window.data1");
_Static_assert(offsetof(SDL_WindowEvent, data2) == 20, "window.data2");
_Static_assert(sizeof(SDL_KeyboardEvent) == 32, "sizeof(SDL_KeyboardEvent)");
_Static_assert(offsetof(SDL_KeyboardEvent, windowID) == 8, "key.windowID");
_Static_assert(offsetof(SDL_KeyboardEvent, state) == 12, "key.state");
_Static_assert(offsetof(SDL_KeyboardEvent, repeat) == 13, "key.repeat");
_Static_assert(offsetof(SDL_KeyboardEvent, keysym) == 16, "key.keysym");
_Static_assert(offsetof(SDL_Keysym, scancode) == 0, "keysym.scancode");
_Static_assert(offsetof(SDL_Keysym, sym) == 4, "keysym.sym");
_Static_assert(offsetof(SDL_Keysym, mod) == 8, "keysym.mod");
_Static_assert(offsetof(SDL_Keysym, unused) == 12, "keysym.unused");
_Static_assert(sizeof(SDL_TextInputEvent) == 44, "sizeof(SDL_TextInputEvent)");
_Static_assert(offsetof(SDL_TextInputEvent, windowID) == 8, "text.windowID");
_Static_assert(offsetof(SDL_TextInputEvent, text) == 12, "text.text");
_Static_assert(sizeof(((SDL_TextInputEvent*)0)->text) == 32, "text size");
_Static_assert(sizeof(SDL_ControllerAxisEvent) == 20, "sizeof(SDL_ControllerAxisEvent)");
_Static_assert(offsetof(SDL_ControllerAxisEvent, which) == 8, "caxis.which");
_Static_assert(offsetof(SDL_ControllerAxisEvent, axis) == 12, "caxis.axis");
_Static_assert(offsetof(SDL_ControllerAxisEvent, value) == 16, "caxis.value");
_Static_assert(sizeof(SDL_ControllerDeviceEvent) == 12, "sizeof(SDL_ControllerDeviceEvent)");
_Static_assert(offsetof(SDL_ControllerDeviceEvent, which) == 8, "cdevice.which");
int main(void) {
EOF
	printf '%s\n' "$MAP" | while read -r goname cname; do
		printf '\tprintf("%s %%lu\\n", (unsigned long)(%s));\n' "$goname" "$cname"
	done
	printf '\treturn 0;\n}\n'
} >"$tmp/c.c"

# shellcheck disable=SC2086
if ! cc -std=c11 -Wall -Werror $cflags -o "$tmp/c" "$tmp/c.c" 2>"$tmp/c.err"; then
	echo "FAIL: the SDL2 headers do not match the struct layouts abi_test.go relies on (or do not compile):" >&2
	cat "$tmp/c.err" >&2
	exit 1
fi
"$tmp/c" | sort >"$tmp/c.out"

# 2. What the Go package says: every numeric constant with an exported name, in decimal.
for f in "$PKG"/*.go; do
	case "$f" in *_test.go) continue ;; esac
	sed -n 's/^[[:space:]]*\([A-Z][A-Za-z0-9]*\)[[:space:]]*=[[:space:]]*\(0[xX][0-9A-Fa-f][0-9A-Fa-f]*\|[0-9][0-9]*\)\([^0-9A-Za-z_].*\)\{0,1\}$/\1 \2/p' "$f"
done | while read -r goname value; do
	printf '%s %s\n' "$goname" "$(printf '%d' "$value")"
done | sort >"$tmp/go.out"

fail=0
# Every Go constant needs a mapping, so a new one cannot skip the check.
while read -r goname _; do
	if ! printf '%s\n' "$MAP" | grep -q "^$goname "; then
		echo "FAIL: the Go constant $goname has no entry in MAP of scripts/check-sdl-constants.sh" >&2
		fail=1
	fi
done <"$tmp/go.out"

# Every mapped constant must equal the header.
while read -r goname cvalue; do
	gvalue=$(sed -n "s/^$goname //p" "$tmp/go.out")
	if [ -z "$gvalue" ]; then
		echo "FAIL: MAP lists $goname but the Go package has no such numeric constant" >&2
		fail=1
	elif [ "$gvalue" != "$cvalue" ]; then
		echo "FAIL: $goname is $gvalue in Go but the SDL2 headers say $cvalue" >&2
		fail=1
	fi
done <"$tmp/c.out"

if [ "$fail" -eq 0 ]; then
	echo "ok: $(wc -l <"$tmp/c.out" | tr -d ' ') constants and the struct layouts match the SDL2 headers"
fi
exit "$fail"
