package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
)

func nativeFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sigstore", name)) //nolint:gosec // fixed public signature fixtures, never caller input.
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestNativeVerifierIdentityAndArtifactBinding(t *testing.T) {
	tr, err := root.NewTrustedRootFromJSON(nativeFixture(t, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	verifier := nativeVerifier{trusted: tr}
	checksums := nativeFixture(t, "checksums.txt")
	for _, tc := range []struct {
		name, file, tag string
		tamper, ok      bool
	}{{"official", "official.json", "v2.0.0", false, true}, {"different repo", "wrong-repo.json", "v2.0.0", false, false}, {"branch identity", "branch.json", "v2.0.0", false, false}, {"wrong issuer", "issuer.json", "v2.0.0", false, false}, {"other tag", "official.json", "v2.0.1", false, false}, {"changed manifest", "official.json", "v2.0.0", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			sums := bytes.Clone(checksums)
			if tc.tamper {
				sums = append(sums, 'x')
			}
			sig, err := verifier.Verify(context.Background(), VerifyInput{Tag: tc.tag, Checksums: sums, Bundle: nativeFixture(t, tc.file)})
			if tc.ok {
				if err != nil || sig != "native" {
					t.Fatalf("verification: %s %v", sig, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
		})
	}
}
func TestNativeVerificationCannotFallBackToChecksumOnly(t *testing.T) {
	verifier := DefaultVerifier(func(string) (string, error) { t.Fatal("native verifier searched for an external tool"); return "", nil })
	for _, body := range [][]byte{nil, []byte("{}"), []byte("not-json")} {
		if sig, err := verifier.Verify(context.Background(), VerifyInput{Tag: "v2.0.0", Checksums: []byte("tampered"), Bundle: body}); err == nil || sig == "none" {
			t.Fatalf("accepted unsigned input: %s %v", sig, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.Verify(ctx, VerifyInput{}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestNativeRejectsMissingTransparencyProof(t *testing.T) {
	tr, err := root.NewTrustedRootFromJSON(nativeFixture(t, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]any
	if err = json.Unmarshal(nativeFixture(t, "official.json"), &bundle); err != nil {
		t.Fatal(err)
	}
	material := bundle["verificationMaterial"].(map[string]any)
	delete(material, "tlogEntries")
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (nativeVerifier{trusted: tr}).Verify(context.Background(), VerifyInput{Tag: "v2.0.0", Checksums: nativeFixture(t, "checksums.txt"), Bundle: body}); err == nil {
		t.Fatal("accepted bundle without transparency evidence")
	}
}
func TestEmbeddedTrustedRootParses(t *testing.T) {
	tr, err := root.NewTrustedRootFromJSON(trustedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.FulcioCertificateAuthorities()) == 0 || len(tr.RekorLogs()) == 0 {
		t.Fatal("embedded root has no release trust material")
	}
}

func TestNativeApplyRejectsTamperedManifest(t *testing.T) {
	u, s := fixture(t)
	tr, err := root.NewTrustedRootFromJSON(nativeFixture(t, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	u.o.Verifier = nativeVerifier{trusted: tr}
	archive := archiveFixture(t, probeFixture(t, ""), "")
	sum := sha256.Sum256(archive)
	// The TLS server supplies a self-consistent archive + manifest, but the manifest is not the signed one.
	sums := fmt.Sprintf("%x  %s\n", sum, archiveName("2.0.0", s.Target, false))
	serve(t, u, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest"):
			w.Header().Set("Location", origin+"/tag/v2.0.0")
			w.WriteHeader(302)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(sums))
		case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
			_, _ = w.Write(nativeFixture(t, "official.json"))
		default:
			_, _ = w.Write(archive)
		}
	})
	_, err = u.Apply(context.Background(), ApplyOptions{Confirm: func(Staged) bool { t.Error("confirmation reached before signature rejection"); return true }})
	errorCode(t, err, "integrity", "signature")
	installed, err := os.ReadFile(u.o.Exe)
	if err != nil || string(installed) != "old" {
		t.Fatal("native signature rejection touched install")
	}
}
