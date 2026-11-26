package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

const archiveLimit = 128 << 20

var extraNotice = regexp.MustCompile(`^THIRD_PARTY/[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // path is a fixed validated member under private staging; O_EXCL refuses existing files.
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
	return extractArchive(b, dir, members(gui), binaryName(gui), runtime.GOOS == "windows")
}

func extractArchive(b []byte, dir string, allowed map[string]bool, executable string, zipped bool) (map[string][]byte, error) {
	seen := map[string][]byte{}
	names := map[string]bool{}
	extras := 0
	total := int64(0)
	directory := func(name string, size int64, mode os.FileMode) (bool, error) {
		if !mode.IsDir() {
			return false, nil
		}
		folded := strings.ToLower(name)
		if mode&os.ModeType != os.ModeDir || name != "THIRD_PARTY/" || size != 0 || names[folded] {
			return false, failure("integrity", "archive_member", nil)
		}
		names[folded] = true
		return true, nil
	}
	add := func(name string, size int64, mode os.FileMode, r io.Reader) error {
		folded := strings.ToLower(name)
		if names[folded] || !mode.IsRegular() {
			return failure("integrity", "archive_member", nil)
		}
		names[folded] = true
		if size < 0 || size > archiveLimit-total {
			return failure("size_mismatch", "expanded_limit", nil)
		}
		if !allowed[name] {
			if !extraNotice.MatchString(name) || extras >= 64 {
				return failure("integrity", "archive_member", nil)
			}
			if size > 1<<20 {
				return failure("size_mismatch", "expanded_limit", nil)
			}
			extras++
			n, err := io.Copy(io.Discard, io.LimitReader(r, size+1))
			if err != nil || n != size {
				return failure("integrity", "archive_read", err)
			}
			total += size
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil || int64(len(data)) != size {
			return failure("integrity", "archive_read", err)
		}
		total += size
		seen[name] = data
		return nil
	}
	if zipped {
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return nil, failure("integrity", "zip", err)
		}
		for _, f := range z.File {
			if f.UncompressedSize64 > archiveLimit {
				return nil, failure("size_mismatch", "expanded_limit", nil)
			}
			skip, e := directory(f.Name, int64(f.UncompressedSize64), f.Mode())
			if e != nil {
				return nil, e
			}
			if skip {
				r, e := f.Open()
				if e != nil {
					return nil, failure("integrity", "archive_member", e)
				}
				n, e := io.Copy(io.Discard, io.LimitReader(r, 1))
				_ = r.Close()
				if n != 0 || e != nil {
					return nil, failure("integrity", "archive_member", e)
				}
				continue
			}
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
		defer func() { _ = gz.Close() }()
		t := tar.NewReader(gz)
		for {
			h, err := t.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, failure("integrity", "tar", err)
			}
			skip, e := directory(h.Name, h.Size, h.FileInfo().Mode())
			if e != nil {
				return nil, e
			}
			if skip {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				return nil, failure("integrity", "tar_type", nil)
			}
			if err = add(h.Name, h.Size, h.FileInfo().Mode(), t); err != nil {
				return nil, err
			}
		}

		// Drain through gzip EOF so CRC/truncation and concatenated streams cannot be hidden after tar's end marker.
		tail, err := io.ReadAll(io.LimitReader(gz, archiveLimit-total+1))
		if err != nil {
			return nil, failure("integrity", "gzip_trailer", err)
		}
		if int64(len(tail)) > archiveLimit-total {
			return nil, failure("size_mismatch", "expanded_limit", nil)
		}
		if len(bytes.Trim(tail, "\x00")) != 0 {
			return nil, failure("integrity", "archive_trailing_data", nil)
		}
	}
	if len(seen) != len(allowed) {
		return nil, failure("integrity", "missing_member", nil)
	}
	for name, data := range seen {
		if name == executable || name == "SDL2.dll" {
			mode := os.FileMode(0755)
			if name == "SDL2.dll" {
				mode = 0644
			}
			if err := writeExclusive(filepath.Join(dir, name), data, mode); err != nil {
				return nil, failure(app.CodeUpdateFailed, "swap_failed", err)
			}
		}
	}
	return seen, nil
}
