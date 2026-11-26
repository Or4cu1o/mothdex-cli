# mothdex-gui

The gamepad front end of mothdex for emulation frontends (Batocera, EmuELEC, RetroBat and others). A separate binary built
`CGO_ENABLED=0` with the `gui` tag; it loads the system SDL2 at run time. Design: `docs/ux/gui.md`; engineering:
`docs/contracts/cli/gui.md`.

```
mothdex-gui [--config file] [--profile id] [--lang xx_yy] [--log-level level] [--window WxH] [--driver name]
mothdex-gui --probe
mothdex-gui --version
mothdex-gui --spike [--driver dummy]
mothdex-gui --help
```

Flags take one dash or two (`-config` and `--config` are the same). There are no positional arguments.

| Flag | Meaning |
|---|---|
| `--config file` | configuration file (default: `MOTHDEX_CONFIG`, else the standard location) |
| `--profile id` | frontend profile (default: detected) |
| `--lang xx_yy` | interface language (default: `interface.language`) |
| `--log-level level` | `debug`, `info`, `warn` or `error`: shortcut for `MOTHDEX_LOG_LEVEL` (default: `log.level`) |
| `--window WxH` | a window of that size instead of the full screen (desktop session) |
| `--driver name` | force an SDL video driver, such as `dummy` |
| `-h`, `--help` | the flags, exit codes and variables, translated (en_us, pt_br and the other complete locales); loads no SDL2 |
| `--probe` | the installer's eligibility check: load SDL2, check the version, open no window, read no config, write no file or log; prints `sdl X.Y.Z` and, on Linux, one word for where SDL2 was loaded from (`system`, `ld_library_path`, `other` or `unknown`, never a path), and exits 0, or the reason (one line, at most 200 bytes) on stderr and exits 8 (not eligible); 1 is an internal error |
| `--version` | print the version line of `mothdex version` (`mothdex 0.1.1 (commit 1a2b3c4, 2026-11-22)`, or `mothdex dev` for a build without ldflags) and exit 0; loads no SDL2 and reads no configuration |
| `--spike` | check the whole SDL2 binding on a hidden window and exit (the CI smoke test); an executable named `mothdex-gui-probe` does this with no flag |

The window carries the moth app icon (`internal/ui/gui/assets/mothdex-icon-256.png`, T-83a) through `SDL_SetWindowIcon`; SDL2 takes one picture per window,
and the X11 and Windows drivers use it (Wayland and the dummy driver ignore it). If it cannot be set, the log says so and the window opens with the default icon.

Exit codes ([`cli-output.md`](../../docs/contracts/cli/cli-output.md) §4.1):

| Code | Meaning |
|---|---|
| 0 | normal exit or SIGTERM/SIGINT |
| 1 | internal error (a panic is logged with its stack) |
| 2 | configuration error: configuration, flags or profile |
| 8 | SDL2 missing or too old, or no display (also when SDL would fall back to `offscreen` or `dummy` without being asked to) |

Releases before 0.2.0 used 1 for SDL, 2 for configuration and 3 for internal errors. A failure of a normal start is
written to the log file and to stderr.

On Windows a console window opens along with the GUI (the build does not use `-H=windowsgui`); this stays for the beta.

Screens so far: Splash, Home, Search with the virtual keyboard, the Platform and Region pickers, Results with covers, Entry with
the link chooser, the Queue, the Start menu, the quit flow and the exit screen. History and Settings arrive in the next pull request;
their tiles say so in a toast. On the Search screen Start runs the search, and Select opens the Queue from any screen.

Build and test:

```
CGO_ENABLED=0 go build -tags gui -o mothdex-gui ./cmd/mothdex-gui
scripts/gui-probe.sh                  # build the probe, inspect the ELF, run it
go test ./internal/ui/gui/...         # model, input, text, loop: no SDL needed
go test -tags gui ./cmd/mothdex-gui   # flags, help, exit codes, --probe
go test -tags gui ./internal/ui/gui/sdl   # binding and headless render; skips without SDL2 (GUI_REQUIRE_SDL=1 fails instead)
```
