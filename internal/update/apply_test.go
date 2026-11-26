package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type verifierFunc func(context.Context, VerifyInput) (string, error)

func (f verifierFunc) Verify(ctx context.Context, r VerifyInput) (string, error) { return f(ctx, r) }
func probeFixture(t *testing.T, fail string) []byte {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName(false))
	cmd := exec.Command("go", "build", "-ldflags=-X main.fail="+fail, "-o", exe, "./testdata/probe")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe build: %v %s", err, out)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func archiveFixture(t *testing.T, binary []byte, extra string) []byte {
	t.Helper()
	files := map[string][]byte{}
	for n := range members(false) {
		files[n] = []byte("fixture")
	}
	files[binaryName(false)] = binary
	files["CHANGELOG.md"] = []byte("## [2.0.0]\nNew version\n## [1.0.0]\nOld version")
	if extra != "" {
		files[extra] = []byte("extra")
	}
	var b bytes.Buffer
	if runtime.GOOS == "windows" {
		z := zip.NewWriter(&b)
		for n, v := range files {
			w, err := z.Create(n)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write(v); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		g := gzip.NewWriter(&b)
		a := tar.NewWriter(g)
		for n, v := range files {
			if err := a.WriteHeader(&tar.Header{Name: n, Mode: 0755, Size: int64(len(v))}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Write(v); err != nil {
				t.Fatal(err)
			}
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}
func TestApplyRiskPaths(t *testing.T) {
	working := probeFixture(t, "")
	badInstalled := probeFixture(t, "installed")
	for _, tc := range []struct {
		name, mode, code, reason string
		restored                 bool
	}{
		{"success", "", "", "", false},
		{"tampered checksums", "checksums", "integrity", "checksum_mismatch", false},
		{"tampered archive", "archive", "integrity", "checksum_mismatch", false},
		{"signature failure", "signature", "integrity", "signature", false},
		{"extra member", "extra", "integrity", "archive_member", false},
		{"probe rollback", "installed", "update_failed", "check_failed", true},
		{"cancel before swap", "cancel", "canceled", "canceled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			binary := working
			if tc.mode == "installed" {
				binary = badInstalled
			}
			extra := ""
			if tc.mode == "extra" {
				extra = "../victim"
			}
			archive := archiveFixture(t, binary, extra)
			hash := sha256.Sum256(archive)
			if tc.mode == "checksums" {
				hash[0] ^= 1
			}
			sums := fmt.Sprintf("%x  %s\n", hash, archiveName("2.0.0", s.Target, false))
			if tc.mode == "archive" {
				archive = append(archive, 0)
			}
			u.o.Verifier = verifierFunc(func(_ context.Context, r VerifyInput) (string, error) {
				if tc.mode == "signature" {
					return "", fmt.Errorf("invalid signature")
				}
				if len(r.Archives) != 1 {
					t.Error("missing archive")
				}
				return "cosign", nil
			})
			serve(t, u, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/latest"):
					w.Header().Set("Location", origin+"/tag/v2.0.0")
					w.WriteHeader(302)
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					_, _ = w.Write([]byte(sums))
				case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
					_, _ = w.Write([]byte("bundle"))
				default:
					_, _ = w.Write(archive)
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result, err := u.Apply(ctx, ApplyOptions{Confirm: func(Staged) bool {
				if tc.mode == "cancel" {
					cancel()
				}
				return true
			}})
			installed, e := os.ReadFile(u.o.Exe)
			if e != nil {
				t.Fatal(e)
			}
			if tc.code != "" {
				errorCode(t, err, tc.code, tc.reason)
				if string(installed) != "old" {
					t.Fatal("installation changed on failure")
				}
				var ae interface{ Error() string }
				_ = ae
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(installed, working) || !result.RestartRequired {
					t.Fatal("new binary missing")
				}
				state, e := u.Managed()
				if e != nil || state.Version != "2.0.0" || state.UpdatedBy != "upgrade" {
					t.Fatalf("state: %+v %v", state, e)
				}
			}
			entries, e := os.ReadDir(s.Dir)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".mothdex-update-") {
					t.Error("staging left behind")
				}
			}
		})
	}
}
