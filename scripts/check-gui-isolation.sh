#!/bin/sh
# Dependency isolation of the GUI (docs/contracts/cli/gui.md section 1): the `mothdex` binary must never
# contain purego or the GUI packages, so that it stays fully static; `mothdex-gui` must contain them.
#
#   scripts/check-gui-isolation.sh
#
# Exit 0 when both hold, 1 when `mothdex` has a GUI dependency, 2 when the check cannot run.
set -eu
cd "$(dirname "$0")/.."

PURE='github.com/ebitengine/purego'
GUI_PKG='mothdex-cli/internal/ui/gui'

command -v go >/dev/null 2>&1 || { echo "check-gui-isolation: go is not installed" >&2; exit 2; }

fail=0

# 1. The import graph of the CLI, for every platform the release builds.
for target in linux/amd64 linux/arm64 linux/arm linux/386 windows/amd64 darwin/arm64; do
	os=${target%/*}
	arch=${target#*/}
	deps=$(CGO_ENABLED=0 GOOS=$os GOARCH=$arch go list -deps ./cmd/mothdex)
	for bad in "$PURE" "$GUI_PKG"; do
		if printf '%s\n' "$deps" | grep -q "$bad"; then
			echo "FAIL: go list -deps ./cmd/mothdex ($target) contains $bad" >&2
			fail=1
		fi
	done
done

# 2. The built binary lists no GUI module either (go version -m reads the embedded build info).
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
CGO_ENABLED=0 go build -o "$tmp/mothdex" ./cmd/mothdex
if go version -m "$tmp/mothdex" | grep -q "$PURE"; then
	echo "FAIL: the mothdex binary embeds $PURE" >&2
	fail=1
fi

# 3. The sanity check of the check: the GUI binary does have them, so a typo above cannot pass by accident.
gdeps=$(CGO_ENABLED=0 go list -deps -tags gui ./cmd/mothdex-gui)
for need in "$PURE" "$GUI_PKG/sdl"; do
	if ! printf '%s\n' "$gdeps" | grep -q "$need"; then
		echo "FAIL: go list -deps -tags gui ./cmd/mothdex-gui lacks $need (the check is not looking at the right thing)" >&2
		fail=1
	fi
done

# 4. The gui tag keeps the GUI out of the default build graph.
if go list -deps ./... 2>/dev/null | grep -q "$GUI_PKG/sdl"; then
	echo "FAIL: ./... without the gui tag reaches $GUI_PKG/sdl" >&2
	fail=1
fi

if [ "$fail" -eq 0 ]; then
	echo "ok: mothdex has no purego and no GUI package; mothdex-gui has both"
fi
exit "$fail"
