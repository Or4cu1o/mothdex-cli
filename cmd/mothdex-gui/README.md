# mothdex-gui

The gamepad front end of mothdex for emulation frontends (Batocera, EmuELEC, RetroBat and others). A separate binary built
`CGO_ENABLED=0` with the `gui` tag; it loads the system SDL2 at run time. Design: `docs/ux/gui.md`; engineering:
`docs/contracts/cli/gui.md`.

```
mothdex-gui [-config file] [-profile id] [-lang xx_yy] [-driver name] [-window WxH]
mothdex-gui -probe [-driver dummy]
```

| Flag | Meaning |
|---|---|
| `-window WxH` | a window of that size instead of the full screen (desktop session) |
| `-driver name` | force an SDL video driver, such as `dummy` |
| `-probe` | check the SDL2 binding on a hidden window and exit (the CI smoke test); an executable named `mothdex-gui-probe` does this with no flag |

Exit codes: 0 normal exit or SIGTERM/SIGINT, 1 SDL missing, too old or no display, 2 configuration error, 3 internal error. Every
failure is written to the log file and to stderr.

This release has the foundation: Splash, Home, the Start menu, the quit flow and the exit screen. Search, Results, Entry, Queue,
History and Settings arrive in the next pull requests; their tiles say so in a toast.

Build and test:

```
CGO_ENABLED=0 go build -tags gui -o mothdex-gui ./cmd/mothdex-gui
scripts/gui-probe.sh                  # build the probe, inspect the ELF, run it
go test ./internal/ui/gui/...         # model, input, text, loop: no SDL needed
go test -tags gui ./internal/ui/gui/sdl   # binding and headless render; skips without SDL2 (GUI_REQUIRE_SDL=1 fails instead)
```
