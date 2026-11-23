### Added

- T-102: the licence notices of the Go modules linked into the binaries. `scripts/gen-go-notices.sh` writes `THIRD_PARTY/go-modules-mothdex.txt` (37 modules, every CLI archive now ships it under `THIRD_PARTY/`) and `THIRD_PARTY/go-modules-mothdex-gui.txt` (14 modules, in the GUI archives) with the full licence files of each module, from the module list of the release build; a module without a licence file fails the run. `Release check` runs `gen-go-notices.sh --check` and `scripts/verify-dist.sh` checks every module of each built binary (`go version -m`) against the file in its archive.

### Changed

- T-102: `install.sh` and `install.ps1` accept the two new notice files (`NOTICE_NAMES`, `$NoticeNames`), and a THIRD_PARTY member they do not know now says that the installer may be older than the release and to fetch it from the release tag.
