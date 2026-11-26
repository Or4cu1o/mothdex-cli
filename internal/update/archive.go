package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

const archiveLimit = 128 << 20

var checksumLine = regexp.MustCompile(`^([a-fA-F0-9]{64}) [ *]([^\s]+)$`)

func checksum(sums []byte, name string) (string, error) {
	found := ""
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		m := checksumLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if m == nil {
			return "", failure("integrity", "checksums_format", nil)
		}
		if m[2] == name {
			if found != "" {
				return "", failure("integrity", "duplicate_checksum", nil)
			}
			found = strings.ToLower(m[1])
		}
	}
	if found == "" {
		return "", failure("integrity", "missing_checksum", nil)
	}
	return found, nil
}
func archiveName(version, target string, gui bool) string {
	name := "mothdex"
	if gui {
		name += "-gui"
	}
	ext := ".tar.gz"
	if strings.HasPrefix(target, "windows_") {
		ext = ".zip"
	}
	return name + "_" + version + "_" + target + ext
}
func binaryName(gui bool) string {
	name := "mothdex"
	if gui {
		name += "-gui"
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
func writeExclusive(path string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	return err
}
func verifyHash(b []byte, expected string) error {
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != expected {
		return failure("integrity", "checksum_mismatch", nil)
	}
	return nil
}

var guiNotices = []string{"README.md", "go-modules-mothdex.txt", "go-modules-mothdex-gui.txt", "SDL2-zlib.txt", "SDL_GameControllerDB-zlib.txt", "purego-Apache-2.0.txt", "x-image-BSD-3-Clause.txt", "NotoSans-OFL-1.1.txt", "Handjet-OFL-1.1.txt"}

func members(gui bool) map[string]bool {
	m := map[string]bool{binaryName(gui): true, "LICENSE": true, "README.md": true, "CHANGELOG.md": true, "THIRD_PARTY/go-modules-mothdex.txt": true}
	if gui {
		for _, n := range guiNotices {
			m["THIRD_PARTY/"+n] = true
		}
		if runtime.GOOS == "windows" {
			m["SDL2.dll"] = true
		}
	}
	return m
}

// extract accepts the exact release member set, never arbitrary paths or archive links.
func extract(b []byte, dir string, gui bool) (map[string][]byte, error) {
	allowed := members(gui)
	seen := map[string][]byte{}
	total := int64(0)
	add := func(name string, size int64, mode os.FileMode, r io.Reader) error {
		if !allowed[name] || seen[name] != nil || !mode.IsRegular() {
			return failure("integrity", "archive_member", fmt.Errorf("unexpected member"))
		}
		if size < 0 || size > archiveLimit-total {
			return failure("size_mismatch", "expanded_limit", nil)
		}
		data, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil || int64(len(data)) != size {
			return failure("integrity", "archive_read", err)
		}
		total += size
		seen[name] = data
		return nil
	}
	if runtime.GOOS == "windows" {
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return nil, failure("integrity", "zip", err)
		}
		for _, f := range z.File {
			r, err := f.Open()
			if err != nil {
				return nil, failure("integrity", "zip_member", err)
			}
			if f.UncompressedSize64 > archiveLimit {
				_ = r.Close()
				return nil, failure("size_mismatch", "expanded_limit", nil)
			}
			err = add(f.Name, int64(f.UncompressedSize64), f.Mode(), r)
			_ = r.Close()
			if err != nil {
				return nil, err
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, failure("integrity", "gzip", err)
		}
		defer gz.Close()
		t := tar.NewReader(gz)
		for {
			h, err := t.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, failure("integrity", "tar", err)
			}
			if h.Typeflag != tar.TypeReg {
				return nil, failure("integrity", "tar_type", nil)
			}
			if err = add(h.Name, h.Size, h.FileInfo().Mode(), t); err != nil {
				return nil, err
			}
		}
	}
	if len(seen) != len(allowed) {
		return nil, failure("integrity", "missing_member", nil)
	}
	for name, data := range seen {
		if name == binaryName(gui) || name == "SDL2.dll" {
			mode := os.FileMode(0755)
			if name == "SDL2.dll" {
				mode = 0644
			}
			if err := writeExclusive(filepath.Join(dir, name), data, mode); err != nil {
				return nil, failure(app.CodeUpdateFailed, "stage_write", err)
			}
		}
	}
	return seen, nil
}
