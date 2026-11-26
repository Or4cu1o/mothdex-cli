package update

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"time"

	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/x509util"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// errTrustMaterial marks a failure that a newer trusted root could explain.
var errTrustMaterial = errors.New("release signed with material the embedded trusted root does not cover")

// signedByRelease reports whether the bundle's certificate names the release identity and its key
// signed exactly these checksums. It proves nothing about the certificate's issuer: it only
// separates a mismatched or altered release from one the embedded root cannot judge.
func signedByRelease(entity *bundle.Bundle, identity verify.CertificateIdentity, digest [sha256.Size]byte) (*x509.Certificate, bool) {
	content, err := entity.VerificationContent()
	if err != nil || content.Certificate() == nil {
		return nil, false
	}
	leaf := content.Certificate()
	summary, err := certificate.SummarizeCertificate(leaf)
	if err != nil || identity.Verify(summary) != nil {
		return nil, false
	}
	signature, err := entity.SignatureContent()
	if err != nil || signature.MessageSignatureContent() == nil {
		return nil, false
	}
	message := signature.MessageSignatureContent()
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !bytes.Equal(message.Digest(), digest[:]) || !ecdsa.VerifyASN1(key, digest[:], message.Signature()) {
		return nil, false
	}
	return leaf, true
}

// staleTrust is true only for a signature of these checksums by the release identity whose
// certificate authority, transparency log or certificate transparency log is unknown to the root,
// or was not valid at signing time. Everything else, including an unknown timestamp authority,
// counts as possible tampering. A forger who controls the release files can still reach this case
// with a private authority: no offline check tells that apart from a real rotation.
func staleTrust(material root.TrustedMaterial, entity *bundle.Bundle, identity verify.CertificateIdentity, digest [sha256.Size]byte) bool {
	leaf, ok := signedByRelease(entity, identity, digest)
	if !ok {
		return false
	}
	entries, err := entity.TlogEntries()
	if err != nil || len(entries) == 0 {
		return false
	}
	if _, err = verify.VerifyLeafCertificate(leaf.NotBefore, leaf, material); err != nil {
		return true
	}
	logged := false
	for _, entry := range entries {
		// Rekor v2 entries carry no integrated time; the certificate's start stands in for it.
		at := entry.IntegratedTime()
		if at.Unix() <= 0 {
			at = leaf.NotBefore
		}
		logged = logged || validAt(material.RekorLogs()[hex.EncodeToString([]byte(entry.LogKeyID()))], at)
	}
	if !logged {
		return true
	}
	scts, err := x509util.ParseSCTsFromCertificate(leaf.Raw)
	if err != nil || len(scts) == 0 {
		return false
	}
	for _, sct := range scts {
		if validAt(material.CTLogs()[hex.EncodeToString(sct.LogID.KeyID[:])], ct.TimestampToTime(sct.Timestamp)) {
			return false
		}
	}
	return true
}

// validAt reports a known log whose key was valid at the given time.
func validAt(log *root.TransparencyLog, at time.Time) bool {
	return log != nil &&
		(log.ValidityPeriodStart.IsZero() || !at.Before(log.ValidityPeriodStart)) &&
		(log.ValidityPeriodEnd.IsZero() || !at.After(log.ValidityPeriodEnd))
}
