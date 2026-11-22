### Added

- T-79: the release (and the snapshot check) builds `mothdex-gui_<version>_<os>_<arch>` archives for linux amd64, arm64, armv7, armv6 and windows amd64 (gui tag,
  `CGO_ENABLED=0`, the armhf loader on arm), with SBOMs, in `checksums.txt` and covered by the same cosign signature and attestation. Members are exactly what the installers
  extract: the binary, `LICENSE`, `README.md`, `CHANGELOG.md`, the files of `THIRD_PARTY/` and, on Windows, the pinned `SDL2.dll` (`scripts/fetch-sdl2-windows.sh`: exact URL,
  SHA-256 checked before extraction, never cached). `scripts/verify-dist.sh` checks members, loader and build flags; `scripts/check-gui-archives.sh` runs `install.sh`
  (real install on amd64, dry runs on the arm archives) and the member rules of `install.ps1` against the snapshot archives and, in the release workflow, the real tag archives; `verify-dist.sh` also refuses non-regular members by type. Not built: GUI for 386, macOS and Windows on Arm.
