### Changed

- T-80 (Bússola): `docs/ux/gui.md` v1.5 §2.3 sets the controller notices:
  - No controller: a once-only `warn` toast, plus a `No controller` top bar badge that is never cut while no pad is
    connected.
  - An unmapped pad: a toast pointing to Settings › About.
  - About shows each pad's name and GUID, and the full `<data dir>/gamecontrollerdb.txt` path for an unmapped pad. The
    path is in the data dir, not the config dir.
  - On 32-bit platforms the GUID reads `Unavailable on this system`.
