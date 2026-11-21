#!/bin/sh
# Build the GUI probe (cmd/mothdex-gui, tag gui, CGO_ENABLED=0) for one target, check its ELF interpreter
# and NEEDED libraries, and run it when the target is the machine's own (docs/contracts/cli/gui.md
# section 2). The CI jobs of the GUI smoke test call this script once per target.
#
#   scripts/gui-probe.sh [GOOS/GOARCH[/GOARM]]      default: the host, e.g. linux/amd64 or linux/arm/7
#
# Environment:
#   SDL_VIDEODRIVER   the video driver of the run (default dummy: no display needed)
#   GUI_PROBE_RUN     1 to run, 0 to only build and inspect (default: run when the target is the host)
#   GUI_PROBE_RUNNER  a prefix to run the binary with, e.g. "qemu-aarch64 -L /usr/aarch64-linux-gnu"
#   DIST              output folder (default dist-gui)
#
# Exit codes: 0 built (and probe ok when run), 1 SDL missing or too old on the machine that ran it,
# 2 the build or the ELF check failed, 3 a check of the probe failed.
set -eu
cd "$(dirname "$0")/.."

target=${1:-$(go env GOOS)/$(go env GOARCH)}
goos=${target%%/*}
rest=${target#*/}
goarch=${rest%%/*}
goarm=""
case "$rest" in */*) goarm=${rest#*/} ;; esac

dist=${DIST:-dist-gui}
name=mothdex-gui-probe
[ "$goos" = windows ] && name=$name.exe
bin=$dist/${goos}_${goarch}${goarm:+v$goarm}/$name
mkdir -p "$(dirname "$bin")"

# The Go default interpreter on linux/arm is wrong for armhf glibc (contract section 2).
ldflags=""
if [ "$goos" = linux ] && [ "$goarch" = arm ]; then
	ldflags="-I /lib/ld-linux-armhf.so.3"
fi

echo "== build $target"
if ! CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOARM=$goarm \
	go build -tags gui -trimpath ${ldflags:+-ldflags "$ldflags"} -o "$bin" ./cmd/mothdex-gui; then
	echo "build failed" >&2
	exit 2
fi
echo "size: $(wc -c <"$bin" | tr -d ' ') bytes ($bin)"

if [ "$goos" = linux ]; then
	if ! command -v readelf >/dev/null 2>&1; then
		echo "readelf is not installed: cannot check the interpreter" >&2
		exit 2
	fi
	echo "== ELF"
	readelf -h "$bin" | grep -E "Class|Machine"
	interp=$(readelf -l "$bin" | sed -n 's/.*Requesting program interpreter: \(.*\)\]/\1/p')
	echo "interpreter: ${interp:-none}"
	# purego needs the dynamic loader to dlopen SDL2: a static probe would only fail at run time.
	if [ -z "$interp" ]; then
		echo "the probe has no program interpreter (it must be a dynamic executable)" >&2
		exit 2
	fi
	readelf -d "$bin" | grep NEEDED || echo "NEEDED: none"
	if [ "$goarch" = arm ] && [ "$interp" != "/lib/ld-linux-armhf.so.3" ]; then
		echo "the arm interpreter must be /lib/ld-linux-armhf.so.3, got ${interp:-none}" >&2
		exit 2
	fi
	# No RPATH or RUNPATH: either one would add a search directory to the dlopen of the SDL2 soname.
	if readelf -d "$bin" | grep -E "RPATH|RUNPATH" >/dev/null; then
		echo "the probe carries an RPATH or RUNPATH" >&2
		exit 2
	fi
	# Only libc-family libraries may be needed: SDL2 itself is found by dlopen at run time.
	if readelf -d "$bin" | grep NEEDED | grep -v -E "lib(c|dl|pthread|m|rt|resolv)\.so" >/dev/null; then
		echo "the probe needs a library besides the libc family" >&2
		exit 2
	fi
fi

host=$(go env GOOS)/$(go env GOARCH)
run=${GUI_PROBE_RUN:-}
if [ -z "$run" ]; then
	if [ "$goos/$goarch" = "$host" ] && [ -z "$goarm" ]; then run=1; else run=0; fi
fi
if [ "$run" != 1 ]; then
	echo "not run here (set GUI_PROBE_RUN=1 and GUI_PROBE_RUNNER to run it under an emulator)"
	exit 0
fi

echo "== run (SDL_VIDEODRIVER=${SDL_VIDEODRIVER:-dummy})"
set +e
# shellcheck disable=SC2086
SDL_VIDEODRIVER=${SDL_VIDEODRIVER:-dummy} ${GUI_PROBE_RUNNER:-} "$bin"
code=$?
set -e
echo "exit code: $code"
exit "$code"
