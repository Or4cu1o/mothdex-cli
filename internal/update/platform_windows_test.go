package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsTrustedOwnerPolicy(t *testing.T) {
	saved := isOwnWindows
	t.Cleanup(func() { isOwnWindows = saved })
	current := "S-1-5-21-100-200-300-1001"
	isOwnWindows = func(sid *windows.SID) bool { return sid.String() == current }
	for _, tc := range []struct {
		sid string
		ok  bool
	}{{current, true}, {"S-1-5-32-544", true}, {"S-1-5-18", true}, {"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464", true}, {"S-1-5-21-100-200-300-1002", false}, {"S-1-5-32-545", false}, {"S-1-1-0", false}} {
		sid, err := windows.StringToSid(tc.sid)
		if err != nil {
			t.Fatal(err)
		}
		if got := trustedWindowsOwner(sid); got != tc.ok {
			t.Errorf("owner %s accepted=%v", tc.sid, got)
		}
	}
}

func TestWindowsAliasIDCommitAndRollback(t *testing.T) {
	working := probeFixture(t, "")
	bad := probeFixture(t, "installed")
	for _, tc := range []struct {
		name            string
		stale, rollback bool
	}{{"commit fresh", false, false}, {"commit stale", true, false}, {"rollback fresh", false, true}, {"rollback stale", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			s.Alias = "mdex"
			s.AliasKind = "hardlink"
			alias := filepath.Join(s.Dir, "mdex.exe")
			source := u.o.Exe
			if tc.stale {
				source = filepath.Join(s.Dir, "previous.exe")
				if err := os.WriteFile(source, []byte("older-alias"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Link(source, alias); err != nil {
				t.Fatal(err)
			}
			if tc.stale {
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			}
			id, err := aliasFileID(alias)
			if err != nil {
				t.Fatal(err)
			}
			s.AliasFileID = id
			writeStateFixture(t, s)
			// Keep the old executable open with the sharing semantics of a running image.
			path, err := windows.UTF16PtrFromString(u.o.Exe)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = windows.CloseHandle(handle) }()
			binary := working
			if tc.rollback {
				binary = bad
			}
			archive := archiveFixture(t, binary, "")
			hash := sha256.Sum256(archive)
			sums := fmt.Sprintf("%x  %s\n", hash, archiveName("2.0.0", s.Target, false))
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
				default:
					_, _ = w.Write(archive)
				}
			})
			_, err = u.Apply(context.Background(), ApplyOptions{Confirm: func(Staged) bool { return true }})
			if tc.rollback {
				errorCode(t, err, "update_failed", "check_failed")
			} else if err != nil {
				t.Fatal(err)
			}
			state, err := u.Managed()
			if err != nil {
				t.Fatal(err)
			}
			wantID, err := aliasFileID(u.o.Exe)
			if err != nil {
				t.Fatal(err)
			}
			if state.AliasFileID != wantID {
				t.Fatalf("recorded alias ID=%s, CLI=%s", state.AliasFileID, wantID)
			}
			a, e := os.Stat(alias)
			if e != nil {
				t.Fatal(e)
			}
			b, e := os.Stat(u.o.Exe)
			if e != nil {
				t.Fatal(e)
			}
			if !os.SameFile(a, b) {
				t.Fatal("alias does not point to committed or restored CLI")
			}
		})
	}
}
