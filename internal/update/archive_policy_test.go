package update

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func releaseZIP(t *testing.T, extra map[string][]byte, directories ...string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/release-windows-layout.zip")
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range r.File {
		if err = z.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range directories {
		if _, err = z.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range extra {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func windowsCLIMembers() map[string]bool {
	return map[string]bool{"mothdex.exe": true, "LICENSE": true, "README.md": true, "CHANGELOG.md": true, "THIRD_PARTY/go-modules-mothdex.txt": true}
}
func TestRealWindowsReleaseLayoutAndBoundedExtras(t *testing.T) {
	for _, tc := range []struct {
		name        string
		extra       map[string][]byte
		directories []string
		code        string
	}{
		{name: "published layout"},
		{name: "empty notice directory", directories: []string{"THIRD_PARTY/"}},
		{name: "future notice", extra: map[string][]byte{"THIRD_PARTY/future-MIT.txt": []byte("ignored notice")}},
		{name: "root injection", extra: map[string][]byte{"future.txt": []byte("bad")}, code: "integrity"},
		{name: "extra case collision", extra: map[string][]byte{"THIRD_PARTY/New.txt": nil, "THIRD_PARTY/new.txt": nil}, code: "integrity"},
		{name: "duplicate executable", extra: map[string][]byte{"mothdex.exe": nil}, code: "integrity"},
		{name: "case collision", extra: map[string][]byte{"third_party/GO-MODULES-MOTHDEX.TXT": []byte("bad")}, code: "integrity"},
		{name: "hidden notice", extra: map[string][]byte{"THIRD_PARTY/.hidden": nil}, code: "integrity"},
		{name: "nested notice", extra: map[string][]byte{"THIRD_PARTY/nested/file": nil}, code: "integrity"},
		{name: "other directory", directories: []string{"elsewhere/"}, code: "integrity"},
		{name: "oversize notice", extra: map[string][]byte{"THIRD_PARTY/large.txt": make([]byte, (1<<20)+1)}, code: "size_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files, err := extractArchive(releaseZIP(t, tc.extra, tc.directories...), dir, windowsCLIMembers(), "mothdex.exe", true)
			if tc.code != "" {
				errorCode(t, err, tc.code, "")
				return
			}
			if err != nil || len(files) != 5 {
				t.Fatalf("extract: count=%d err=%v", len(files), err)
			}
			entries, e := os.ReadDir(dir)
			if e != nil || len(entries) != 1 || entries[0].Name() != "mothdex.exe" {
				t.Fatalf("extras written: %v %v", entries, e)
			}
			if _, e = os.Stat(filepath.Join(dir, "mothdex.exe")); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, count := range []int{64, 65} {
		extra := map[string][]byte{}
		for i := 0; i < count; i++ {
			extra[fmt.Sprintf("THIRD_PARTY/notice-%02d", i)] = nil
		}
		_, err := extractArchive(releaseZIP(t, extra), t.TempDir(), windowsCLIMembers(), "mothdex.exe", true)
		if count == 64 && err != nil {
			t.Fatal(err)
		}
		if count == 65 {
			errorCode(t, err, "integrity", "archive_member")
		}
	}
}

func TestArchiveRejectsSymlinkDirectoryEntry(t *testing.T) {
	b := releaseZIP(t, nil)
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range r.File {
		if err = z.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	header := zip.FileHeader{Name: "THIRD_PARTY/", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0700)
	if _, err = z.CreateHeader(&header); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = extractArchive(out.Bytes(), t.TempDir(), windowsCLIMembers(), "mothdex.exe", true)
	errorCode(t, err, "integrity", "archive_member")
}
