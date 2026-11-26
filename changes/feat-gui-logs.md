### Added

- T-115: `mothdex-gui` writes a session log and `latest.log`. A bad command line or a configuration that does not load
  is logged in the default log folder, so a Port with no terminal still leaves something to send.
- T-115: every error the GUI shows has a `ref`, on screen and in the log, next to its code and the log path.
- T-115: after a panic, `crash/` holds the stack and the GUI shows a screen with the ref, "Export logs" and "Quit".
- T-115: "Export logs" in Settings > About writes the zip to attach to a report and says where it is.

### Security

- T-115: the panic value and stack of `mothdex-gui` are written only through the log redactor, and stderr no longer
  prints the value.
