//go:build ignore

// Generates public, offline signature fixtures. Private keys exist only in memory and are discarded.
// Tests consume these static files and never depend on the wall clock or a service.
package main

import (
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	common "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	rekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		return err
	}
	issuer := "https://token.actions.githubusercontent.com"
	identity := "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/v2.0.0"
	artifact := []byte("offline signed checksums fixture\n")
	for _, tc := range []struct{ name, identity, issuer string }{{"official", identity, issuer}, {"wrong-repo", "https://github.com/evil/mothdex-cli/.github/workflows/release.yml@refs/tags/v2.0.0", issuer}, {"branch", "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/heads/main", issuer}, {"issuer", identity, "https://evil.example"}, {"workflow", "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/ci.yml@refs/tags/v2.0.0", issuer}} {
		e, err := vs.SignWithVersion(tc.identity, tc.issuer, artifact, "v0.1")
		if err != nil {
			return err
		}
		vc, err := e.VerificationContent()
		if err != nil {
			return err
		}
		sc, err := e.SignatureContent()
		if err != nil {
			return err
		}
		entries, err := e.TlogEntries()
		if err != nil {
			return err
		}
		logs := make([]*rekor.TransparencyLogEntry, 0, len(entries))
		for _, entry := range entries {
			log := entry.TransparencyLogEntry()
			signed, err := vs.RekorSignPayload(tlog.RekorPayload{Body: base64.StdEncoding.EncodeToString(log.CanonicalizedBody), IntegratedTime: log.IntegratedTime, LogIndex: log.LogIndex, LogID: hex.EncodeToString(log.LogId.KeyId)})
			if err != nil {
				return err
			}
			log.InclusionPromise = &rekor.InclusionPromise{SignedEntryTimestamp: signed}
			log.KindVersion = &rekor.KindVersion{Kind: "hashedrekord", Version: "0.0.1"}
			logs = append(logs, log)
		}
		msg := sc.MessageSignatureContent()
		bundle := &pb.Bundle{MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1", VerificationMaterial: &pb.VerificationMaterial{Content: &pb.VerificationMaterial_X509CertificateChain{X509CertificateChain: &common.X509CertificateChain{Certificates: []*common.X509Certificate{{RawBytes: vc.Certificate().Raw}}}}, TlogEntries: logs}, Content: &pb.Bundle_MessageSignature{MessageSignature: &common.MessageSignature{MessageDigest: &common.HashOutput{Algorithm: common.HashAlgorithm_SHA2_256, Digest: msg.Digest()}, Signature: msg.Signature()}}}
		b, err := protojson.Marshal(bundle)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join("internal/update/testdata/sigstore", tc.name+".json"), b, 0600); err != nil {
			return err
		}
	}
	logs, cts := vs.RekorLogs(), vs.CTLogs()
	for _, m := range []map[string]*root.TransparencyLog{logs, cts} {
		for id, log := range m {
			raw, e := hex.DecodeString(id)
			if e != nil {
				return e
			}
			log.ID = raw
			log.SignatureHashFunc = crypto.SHA256
		}
	}
	tr, err := root.NewTrustedRoot(root.TrustedRootMediaType01, vs.FulcioCertificateAuthorities(), cts, vs.TimestampingAuthorities(), logs)
	if err != nil {
		return err
	}
	b, err := tr.MarshalJSON()
	if err != nil {
		return err
	}
	if err = os.WriteFile("internal/update/testdata/sigstore/root.json", b, 0600); err != nil {
		return err
	}
	return os.WriteFile("internal/update/testdata/sigstore/checksums.txt", artifact, 0600)
}
