package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

func nativeFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sigstore", name)) //nolint:gosec // fixed public signature fixtures, never caller input.
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The synthetic bundles come from a test authority with no SCT, so the identity rule is tested alone.
func TestNativeIdentityBinding(t *testing.T) {
	checksums := nativeFixture(t, "checksums.txt")
	for _, tc := range []struct {
		name, file, tag string
		tamper, ok      bool
	}{{"official", "official.json", "v2.0.0", false, true}, {"different repo", "wrong-repo.json", "v2.0.0", false, false}, {"branch identity", "branch.json", "v2.0.0", false, false}, {"wrong issuer", "issuer.json", "v2.0.0", false, false}, {"other workflow", "workflow.json", "v2.0.0", false, false}, {"other tag", "official.json", "v2.0.1", false, false}, {"changed manifest", "official.json", "v2.0.0", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			sums := bytes.Clone(checksums)
			if tc.tamper {
				sums = append(sums, 'x')
			}
			var entity bundle.Bundle
			if err := entity.UnmarshalJSON(nativeFixture(t, tc.file)); err != nil {
				t.Fatal(err)
			}
			identity, err := releaseIdentity(tc.tag)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := signedByRelease(&entity, identity, sha256.Sum256(sums)); ok != tc.ok {
				t.Fatalf("signedByRelease = %v, want %v", ok, tc.ok)
			}
		})
	}
}

func TestNativeRequiresSCT(t *testing.T) {
	tr, err := root.NewTrustedRootFromJSON(nativeFixture(t, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = (nativeVerifier{trusted: tr}).Verify(context.Background(), VerifyInput{Tag: "v2.0.0", Checksums: nativeFixture(t, "checksums.txt"), Bundle: nativeFixture(t, "official.json")})
	if err == nil || !strings.Contains(err.Error(), "SCT") {
		t.Fatalf("bundle without SCT: %v", err)
	}
	if hint := signatureHint(err); hint != "update.tamper_hint" {
		t.Fatalf("hint = %s", hint)
	}
}

func releaseFixture(t *testing.T, tag string) VerifyInput {
	t.Helper()
	return VerifyInput{Tag: tag, Checksums: nativeFixture(t, "release/"+tag+"/checksums.txt"), Bundle: nativeFixture(t, "release/"+tag+"/checksums.txt.sigstore.json")}
}

func TestNativeVerifiesPublishedReleases(t *testing.T) {
	for _, tag := range []string{"v0.1.0", "v0.1.1-rc.1", "v0.1.1"} {
		t.Run(tag, func(t *testing.T) {
			if sig, err := DefaultVerifier().Verify(context.Background(), releaseFixture(t, tag)); err != nil || sig != "native" {
				t.Fatalf("published release refused: %q %v", sig, err)
			}
		})
	}
}

// A release that does not match what was signed is never answered with "reinstall".
func TestNativeTamperedReleaseIsNotATrustError(t *testing.T) {
	for name, change := range map[string]func(*VerifyInput){
		"tag swap": func(in *VerifyInput) { in.Tag = "v0.1.0" },
		"tampered checksums": func(in *VerifyInput) {
			in.Checksums = bytes.Clone(in.Checksums)
			in.Checksums[0] ^= 1
		},
		"no transparency entry": func(in *VerifyInput) {
			in.Bundle = editJSON(t, in.Bundle, func(b map[string]any) { delete(b["verificationMaterial"].(map[string]any), "tlogEntries") })
		},
		"missing bundle": func(in *VerifyInput) { in.Bundle = nil },
	} {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s stale=%v", name, stale), func(t *testing.T) {
				verifier := nativeVerifier{}
				if stale {
					verifier.trusted = editedRoot(t, func(r map[string]any) { delete(r, "ctlogs") })
				}
				in := releaseFixture(t, "v0.1.1")
				change(&in)
				_, err := verifier.Verify(context.Background(), in)
				if err == nil || errors.Is(err, errTrustMaterial) || signatureHint(err) != "update.tamper_hint" {
					t.Fatalf("want a tampering error, got %v", err)
				}
			})
		}
	}
}

func TestNativeStaleRootIsATrustError(t *testing.T) {
	expire := func(list string) func(map[string]any) {
		return func(r map[string]any) {
			for _, item := range r[list].([]any) {
				entry := item.(map[string]any)
				if key, ok := entry["publicKey"].(map[string]any); ok {
					entry = key
				}
				entry["validFor"] = map[string]any{"start": "2021-01-01T00:00:00Z", "end": "2021-12-31T00:00:00Z"}
			}
		}
	}
	for name, change := range map[string]func(map[string]any){
		"unknown authority":        func(r map[string]any) { delete(r, "certificateAuthorities") },
		"unknown transparency log": func(r map[string]any) { delete(r, "tlogs") },
		"unknown certificate log":  func(r map[string]any) { delete(r, "ctlogs") },
		"expired authority":        expire("certificateAuthorities"),
		"expired transparency log": expire("tlogs"),
		"expired certificate log":  expire("ctlogs"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (nativeVerifier{trusted: editedRoot(t, change)}).Verify(context.Background(), releaseFixture(t, "v0.1.1"))
			if !errors.Is(err, errTrustMaterial) || signatureHint(err) != "update.verification_hint" {
				t.Fatalf("want a trust material error, got %v", err)
			}
		})
	}
}

func editJSON(t *testing.T, body []byte, change func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	change(value)
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func editedRoot(t *testing.T, change func(map[string]any)) *root.TrustedRoot {
	t.Helper()
	tr, err := root.NewTrustedRootFromJSON(editJSON(t, trustedRoot, change))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestNativeVerificationCannotFallBackToChecksumOnly(t *testing.T) {
	verifier := DefaultVerifier()
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
func TestEmbeddedTrustedRootIsPinned(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256(trustedRoot)); got != trustedRootSHA256 {
		t.Fatalf("trusted-root.json is %s, pinned %s: review the change, then update the pin and docs/release.md", got, trustedRootSHA256)
	}
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
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Args["hint_key"] != "update.tamper_hint" {
		t.Fatalf("tampered manifest must not advise reinstalling: %v", err)
	}
	installed, err := os.ReadFile(u.o.Exe)
	if err != nil || string(installed) != "old" {
		t.Fatal("native signature rejection touched install")
	}
}

func TestMissingGUIInTamperedManifestDoesNotAdviseReinstall(t *testing.T) {
	u, s := fixture(t)
	s.GUI = true
	writeStateFixture(t, s)
	before, err := os.ReadFile(filepath.Join(s.Dir, stateName))
	if err != nil {
		t.Fatal(err)
	}
	archive := archiveFixture(t, []byte("must never execute"), "")
	digest := sha256.Sum256(archive)
	// Self-consistent CLI archive, but the manifest is unsigned and omits the GUI line.
	sums := []byte(fmt.Sprintf("%x  %s\n", digest, archiveName("2.0.0", s.Target, false)))
	trusted, err := root.NewTrustedRootFromJSON(nativeFixture(t, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	u.o.Verifier = nativeVerifier{trusted: trusted}
	bundle := nativeFixture(t, "official.json")
	serve(t, u, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest"):
			w.Header().Set("Location", origin+"/tag/v2.0.0")
			w.WriteHeader(302)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write(sums)
		case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
			_, _ = w.Write(bundle)
		default:
			_, _ = w.Write(archive)
		}
	})
	_, err = u.Apply(context.Background(), ApplyOptions{Confirm: func(Staged) bool { t.Fatal("confirmation after tampered manifest"); return true }})
	errorCode(t, err, "integrity", "signature")
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Args["hint_key"] != "update.tamper_hint" || ae.Args["restored"] != true {
		t.Fatalf("unsafe recovery advice: %v", err)
	}
	after, e := os.ReadFile(filepath.Join(s.Dir, stateName))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("tampered manifest changed install state")
	}
	installed, e := os.ReadFile(u.o.Exe)
	if e != nil || string(installed) != "old" {
		t.Fatal("tampered manifest changed executable")
	}
}
