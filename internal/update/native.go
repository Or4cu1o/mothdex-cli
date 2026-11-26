package update

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// trustedRoot is obtained through Sigstore TUF by scripts/update-trusted-root.go.
// Verification is offline: no certificate, transparency-log or TUF URLs from a release are fetched.
//
//go:embed trusted-root.json
var trustedRoot []byte

type nativeVerifier struct{ trusted root.TrustedMaterial }

func (v nativeVerifier) Verify(ctx context.Context, r VerifyInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(r.Tag) < 2 || r.Tag[0] != 'v' || !validVersion(r.Tag[1:]) {
		return "", fmt.Errorf("invalid release tag")
	}
	if len(r.Bundle) == 0 || len(r.Bundle) > 256<<10 || len(r.Checksums) > 64<<10 {
		return "", fmt.Errorf("missing or oversized signature material")
	}
	material := v.trusted
	if material == nil {
		tr, err := root.NewTrustedRootFromJSON(trustedRoot)
		if err != nil {
			return "", fmt.Errorf("embedded trusted root: %w", err)
		}
		material = tr
	}
	var entity bundle.Bundle
	if err := entity.UnmarshalJSON(r.Bundle); err != nil {
		return "", fmt.Errorf("signature bundle: %w", err)
	}
	// Require verified transparency-log evidence and an authenticated observer time to validate the short-lived certificate.
	// Neither current wall-clock validation nor untrusted timestamps can substitute for these proofs.
	verifier, err := verify.NewVerifier(material, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return "", err
	}
	identity, err := verify.NewShortCertificateIdentity("https://token.actions.githubusercontent.com", "", "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/"+r.Tag, "")
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(r.Checksums)
	_, err = verifier.Verify(&entity, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest[:]), verify.WithCertificateIdentity(identity)))
	if err != nil {
		return "", fmt.Errorf("release signature or identity: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	return "native", nil
}
