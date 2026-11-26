### Security

- T-116, D35: upgrades verify release origin natively with sigstore-go and an embedded, TUF-verified Public Good root;
  require the exact official release workflow identity and tag. Missing tools no longer permit SHA-256-only upgrades,
  and an invalid bundle never falls back to a weaker verifier.
