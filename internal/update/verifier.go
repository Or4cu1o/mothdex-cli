package update

import "context"

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

// DefaultVerifier always verifies natively. LookPath is retained for source compatibility with Options.
// No external tool or checksum-only path is selected after a signature verification failure.
func DefaultVerifier(_ func(string) (string, error)) Verifier { return nativeVerifier{} }
