### Fixed

- T-100: RAM-disk warnings follow the selected language in startup warnings, CLI profile output and the GUI System card, in all seven locales.
- T-100: a ROM root or data folder that does not exist yet inherits the nearest existing ancestor's RAM-disk warning; compose wiring is covered by regression tests.

### Changed

- T-100: document the deliberate POSIX versus C locale fallback choice, use `search --page-size` in the checklist, and close the already fixed T-22 F3 terminal finding.
