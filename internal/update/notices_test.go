package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoticesUseInstallerFolderAndPreserveUnknownFiles(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "replace known", true: "preserve foreign"}[foreign], func(t *testing.T) {
			u, s := fixture(t)
			dest := filepath.Join(s.Dir, "mothdex-licenses")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(dest, "go-modules-mothdex.txt")
			if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			if foreign {
				if err := os.WriteFile(filepath.Join(dest, "personal.txt"), []byte("mine"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			rootLicense := filepath.Join(s.Dir, "LICENSE")
			if err := os.WriteFile(rootLicense, []byte("personal license"), 0600); err != nil {
				t.Fatal(err)
			}
			u.writeNotices(s.Dir, map[string][]byte{"go-modules-mothdex.txt": []byte("new")})
			//nolint:gosec // Known licence file inside the private installation fixture.
			got, err := os.ReadFile(old)
			want := "new"
			if foreign {
				want = "old"
			}
			if err != nil || string(got) != want {
				t.Fatalf("notice=%q err=%v, want %q", got, err, want)
			}
			//nolint:gosec // Known file inside the private installation fixture.
			got, err = os.ReadFile(rootLicense)
			if err != nil || string(got) != "personal license" {
				t.Fatal("root LICENSE changed")
			}
			if _, err = os.Stat(filepath.Join(s.Dir, "go-modules-mothdex.txt")); !os.IsNotExist(err) {
				t.Fatal("wrote root notice")
			}
		})
	}
}

func TestNoticeLinksAndNonRegularNamesAreUntouched(t *testing.T) {
	for _, kind := range []string{"folder link", "file link", "subfolder"} {
		t.Run(kind, func(t *testing.T) {
			u, s := fixture(t)
			outside := t.TempDir()
			target := filepath.Join(outside, "private.txt")
			if err := os.WriteFile(target, []byte("mine"), 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(s.Dir, "mothdex-licenses")
			if kind == "folder link" {
				if err := os.Symlink(outside, dest); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else {
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dest, "go-modules-mothdex.txt")
				if kind == "file link" {
					if err := os.Symlink(target, path); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				} else if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			u.writeNotices(s.Dir, map[string][]byte{"go-modules-mothdex.txt": []byte("new")})
			b, err := os.ReadFile(target) //nolint:gosec // Private fixture file outside the installation.
			if err != nil || string(b) != "mine" {
				t.Fatal("followed licence destination link")
			}
			st, err := os.Lstat(dest)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "folder link" && st.Mode()&os.ModeSymlink == 0 {
				t.Fatal("replaced foreign directory link")
			}
		})
	}
}
