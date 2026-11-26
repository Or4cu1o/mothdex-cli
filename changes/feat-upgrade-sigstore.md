### Security

- T-116, D35: upgrades verify release origin natively with sigstore-go and an embedded, TUF-verified Public Good root;
  require the exact official release workflow identity and tag. Missing tools no longer permit SHA-256-only upgrades,
  and an invalid bundle never falls back to a weaker verifier.
- T-116b: the verifier also requires a certificate transparency proof (SCT); the trusted root is pinned by SHA-256; the
  reinstall hint appears only when the embedded root may be out of date, and any other failure warns of possible
  tampering.
