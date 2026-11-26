package update

import (
	"context"
	"errors"
)

// Verifier checks release origin independently of the mandatory archive hashes.
type Verifier interface {
	Verify(context.Context, VerifyInput) (string, error)
}

// VerifyInput binds checksums and every downloaded archive to an exact release tag.
type VerifyInput struct {
	Tag               string
	Checksums, Bundle []byte
	Archives          map[string]string
}

// DefaultVerifier always verifies natively (D35). No external tool or checksum-only path is ever selected.
func DefaultVerifier() Verifier { return nativeVerifier{} }

// signatureHint picks the advice for a failed verification. Reinstalling is advised only when the
// embedded trusted root may be out of date: the installers can be SHA-256 only, so a release that
// looks tampered with must never send people to them.
func signatureHint(err error) string {
	if errors.Is(err, errTrustMaterial) {
		return "update.verification_hint"
	}
	return "update.tamper_hint"
}
