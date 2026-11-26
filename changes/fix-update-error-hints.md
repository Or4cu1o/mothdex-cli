### Fixed

- T-118: updater failures use shared localized update messages and recovery hints instead of raw reasons or game
  download guidance. Errors before replacement say the previous installation is intact; signature failures retain
  the tampering or trusted-root advice. Preserve explicit installer hints for unmanaged installs, missing GUI assets
  and incomplete rollback. Remove the documentation-text test and document the Windows ACE SID pointer conversion.
