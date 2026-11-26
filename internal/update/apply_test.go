package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

type verifierFunc func(context.Context, VerifyInput) (string, error)

func (f verifierFunc) Verify(ctx context.Context, r VerifyInput) (string, error) { return f(ctx, r) }
func probeFixture(t *testing.T, fail string) []byte {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName(false))
	cmd := exec.CommandContext(context.Background(), "go", "build", "-ldflags=-X main.fail="+fail, "-o", exe, "./testdata/probe") //nolint:gosec // fixture build uses test constants and a private temporary output path.
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe build: %v %s", err, out)
	}
	b, err := os.ReadFile(exe) //nolint:gosec // generated binary fixture in t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func archiveFixture(t *testing.T, binary []byte, extra string, gui ...bool) []byte {
	t.Helper()
	isGUI := len(gui) > 0 && gui[0]
	files := map[string][]byte{}
	for n := range members(isGUI) {
		files[n] = []byte("fixture")
	}
	files[binaryName(isGUI)] = binary
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
	badStage := probeFixture(t, "all")
	for _, tc := range []struct {
		name, mode, code, reason string
		restored                 bool
	}{
		{"success", "", "", "", false},
		{"tampered checksums", "checksums", "integrity", "checksum_mismatch", false},
		{"tampered archive", "archive", "integrity", "checksum_mismatch", false},
		{"signature failure", "signature", "integrity", "signature", false},
		{"extra member", "extra", "integrity", "archive_member", false},
		{"staged probe", "staged", "update_failed", "check_failed", true},
		{"probe rollback", "installed", "update_failed", "check_failed", true},
		{"cancel before swap", "cancel", "canceled", "canceled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			binary := working
			if tc.mode == "staged" {
				binary = badStage
			}
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
				if tc.restored {
					var ae *app.Error
					if !errors.As(err, &ae) || ae.Args["restored"] != true {
						t.Fatalf("rollback not reported: %v", err)
					}
				}
				if string(installed) != "old" {
					t.Fatal("installation changed on failure")
				}

			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(installed, working) || !result.RestartRequired {
					t.Fatal("new binary missing")
				}
				for _, n := range []string{"LICENSE", "THIRD_PARTY", "mothdex-licenses"} {
					if _, e := os.Lstat(filepath.Join(s.Dir, n)); !os.IsNotExist(e) {
						t.Fatalf("CLI-only wrote licence path %s: %v", n, e)
					}
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

func TestGUIIsVerifiedBeforeAnySwapAndRollsBackTogether(t *testing.T) {
	working := probeFixture(t, "")
	badInstalled := probeFixture(t, "installed")
	for _, tamper := range []bool{true, false} {
		t.Run(fmt.Sprint("tamper=", tamper), func(t *testing.T) {
			u, s := fixture(t)
			s.GUI = true
			writeStateFixture(t, s)
			guiPath := filepath.Join(s.Dir, binaryName(true))
			if err := os.WriteFile(guiPath, []byte("old-gui"), 0700); err != nil { //nolint:gosec // executable GUI fixture.
				t.Fatal(err)
			} //nolint:gosec // executable GUI fixture.
			if runtime.GOOS == "windows" {
				if err := os.WriteFile(filepath.Join(s.Dir, "SDL2.dll"), []byte("old-dll"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cli := archiveFixture(t, badInstalled, "")
			gui := archiveFixture(t, working, "", true)
			cliName, guiName := archiveName("2.0.0", s.Target, false), archiveName("2.0.0", s.Target, true)
			cliSum, guiSum := sha256.Sum256(cli), sha256.Sum256(gui)
			if tamper {
				guiSum[0] ^= 1
			}
			sums := fmt.Sprintf("%x  %s\n%x  %s\n", cliSum, cliName, guiSum, guiName)
			u.o.Verifier = verifierFunc(func(context.Context, VerifyInput) (string, error) { return "cosign", nil })
			serve(t, u, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/latest"):
					w.Header().Set("Location", origin+"/tag/v2.0.0")
					w.WriteHeader(302)
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					_, _ = w.Write([]byte(sums))
				case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
					_, _ = w.Write([]byte("fixture"))
				case strings.HasSuffix(r.URL.Path, guiName):
					_, _ = w.Write(gui)
				default:
					_, _ = w.Write(cli)
				}
			})
			_, err := u.Apply(context.Background(), ApplyOptions{Confirm: func(Staged) bool { return true }})
			if tamper {
				errorCode(t, err, "integrity", "checksum_mismatch")
			} else {
				errorCode(t, err, "update_failed", "check_failed")
			}
			for name, want := range map[string]string{binaryName(false): "old", binaryName(true): "old-gui"} {
				b, e := os.ReadFile(filepath.Join(s.Dir, name)) //nolint:gosec // fixed component names in temporary installation fixture.
				if e != nil || string(b) != want {
					t.Fatalf("component not restored: %s %v", name, e)
				}
			}
			if runtime.GOOS == "windows" {
				b, e := os.ReadFile(filepath.Join(s.Dir, "SDL2.dll"))
				if e != nil || string(b) != "old-dll" {
					t.Fatal("DLL not restored")
				}
			}
		})
	}
}

func TestReleaseNotesOnlyNewerVersionSections(t *testing.T) {
	input := "# Changelog\nIntroduction\n## [Unreleased]\nUnshipped\n## [3.0.0] - date\nNew\n### Fixed\nFix\n## [1.0.0]\nOld\n## [4.0.0]\nNewer section later\n"
	want := "## [3.0.0] - date\nNew\n### Fixed\nFix\n## [4.0.0]\nNewer section later\n"
	if got := releaseNotes([]byte(input), "2.0.0"); got != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

func TestStagedGUIFailureReasonsAndRestoredState(t *testing.T) {
	working := probeFixture(t, "")
	for _, tc := range []struct{ mode, reason string }{{"version", "check_failed"}, {"probe1", "check_failed"}, {"probe8", "gui_not_eligible"}, {"missing", "gui_missing"}} {
		t.Run(tc.mode, func(t *testing.T) {
			u, s := fixture(t)
			s.GUI = true
			writeStateFixture(t, s)
			//nolint:gosec // Executable GUI installation fixture.
			if err := os.WriteFile(filepath.Join(s.Dir, binaryName(true)), []byte("old-gui"), 0700); err != nil {
				t.Fatal(err)
			} //nolint:gosec // executable installation fixture.
			cliName, guiName := archiveName("2.0.0", s.Target, false), archiveName("2.0.0", s.Target, true)
			cli := archiveFixture(t, working, "")
			gui := archiveFixture(t, probeFixture(t, tc.mode), "", true)
			cliSum, guiSum := sha256.Sum256(cli), sha256.Sum256(gui)
			sums := fmt.Sprintf("%x  %s\n", cliSum, cliName)
			if tc.mode != "missing" {
				sums += fmt.Sprintf("%x  %s\n", guiSum, guiName)
			}
			u.o.Verifier = verifierFunc(func(context.Context, VerifyInput) (string, error) { return "cosign", nil })
			serve(t, u, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/latest"):
					w.Header().Set("Location", origin+"/tag/v2.0.0")
					w.WriteHeader(302)
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					_, _ = w.Write([]byte(sums))
				case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
					_, _ = w.Write([]byte("fixture"))
				case strings.HasSuffix(r.URL.Path, guiName):
					_, _ = w.Write(gui)
				default:
					_, _ = w.Write(cli)
				}
			})
			_, err := u.Apply(context.Background(), ApplyOptions{Confirm: func(Staged) bool { t.Fatal("prompt after failed staging"); return true }})
			errorCode(t, err, "update_failed", tc.reason)
			var ae *app.Error
			if !errors.As(err, &ae) || ae.Args["restored"] != true {
				t.Fatalf("intact installation not reported: %v", err)
			}
			if tc.mode == "missing" && ae.Args["hint_key"] != nil {
				t.Fatal("missing GUI lost installer hint")
			}
			if tc.mode != "missing" && ae.Args["hint_key"] != "update.unchanged_hint" {
				t.Fatal("staging error advises reinstall")
			}
			for name, want := range map[string]string{binaryName(false): "old", binaryName(true): "old-gui"} {
				//nolint:gosec // Component names come from the fixed test table.
				got, e := os.ReadFile(filepath.Join(s.Dir, name))
				if e != nil || string(got) != want {
					t.Fatalf("staging changed %s: %q %v", name, got, e)
				} //nolint:gosec // known fixture paths.
			}
		})
	}
}
func TestInProgressKeepsPreviousVersion(t *testing.T) {
	u, s := fixture(t)
	unlock, err := acquireLock(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = u.Apply(context.Background(), ApplyOptions{})
	errorCode(t, err, "update_failed", "in_progress")
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Args["restored"] != true {
		t.Fatal("held lock implies broken installation")
	}
}

func TestIncompleteRollbackRequiresReinstall(t *testing.T) {
	u, s := fixture(t)
	stage := t.TempDir()
	path := filepath.Join(stage, binaryName(false))
	//nolint:gosec // Executable probe fixture, never a downloaded or user-controlled program.
	if err := os.WriteFile(path, probeFixture(t, "all"), 0700); err != nil {
		t.Fatal(err)
	}
	err := u.swap(context.Background(), ApplyOptions{Progress: func(p Progress) {
		if p.Step == "check" {
			if err := os.Remove(u.o.Exe + ".old"); err != nil {
				t.Fatal(err)
			}
		}
	}}, s, stage, stagedRelease{version: "2.0.0", files: []string{binaryName(false)}})
	errorCode(t, err, "update_failed", "check_failed")
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Args["restored"] != false || ae.Args["hint_key"] != nil || ae.Args["install_command"] != InstallCommand() {
		t.Fatalf("incomplete rollback hint: %v", err)
	}
}
