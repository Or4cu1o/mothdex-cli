// Package zipfix builds hostile and edge-case zip archives for the downloader
// verification tests (T-16 cases 41 to 43). Nothing is extracted to disk.
package zipfix

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"sort"
)

// Valid returns a well-formed zip with one stored (uncompressed) member per
// entry, in sorted name order so the bytes are the same on every run.
func Valid(members map[string][]byte) []byte {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		must(err)
		_, err = w.Write(members[name])
		must(err)
	}
	must(zw.Close())
	return buf.Bytes()
}

// ManyEmpty returns a zip with n empty members (member-count cap, 65 535).
func ManyEmpty(n int) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < n; i++ {
		_, err := zw.CreateHeader(&zip.FileHeader{Name: memberName(i), Method: zip.Store})
		must(err)
	}
	must(zw.Close())
	return buf.Bytes()
}

// DeclaredSizeLie returns a zip whose single member inflates to more bytes
// than its header declares: reading it to EOF must fail the size/CRC check.
func DeclaredSizeLie(actual, declared int) []byte {
	data := bytes.Repeat([]byte{'A'}, actual)
	comp := deflate(data)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: "lie.bin", Method: zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(comp)),
		UncompressedSize64: uint64(declared),
	})
	must(err)
	_, err = w.Write(comp)
	must(err)
	must(zw.Close())
	return buf.Bytes()
}

// Bomb returns a zip with one deflate member that inflates to n zero bytes
// (over 100:1; deflate tops out near 1000:1), with an honest header, so only a cumulative cap stops it.
// Building it streams n bytes through the compressor: keep n modest and reuse
// the result across tests.
func Bomb(n int64) []byte {
	var comp bytes.Buffer
	fw, err := flate.NewWriter(&comp, flate.BestSpeed)
	must(err)
	crc := crc32.NewIEEE()
	zeros := make([]byte, 1<<20)
	for left := n; left > 0; {
		chunk := int64(len(zeros))
		if left < chunk {
			chunk = left
		}
		_, err = fw.Write(zeros[:chunk])
		must(err)
		_, _ = crc.Write(zeros[:chunk])
		left -= chunk
	}
	must(fw.Close())
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: "bomb.bin", Method: zip.Deflate, CRC32: crc.Sum32(),
		CompressedSize64: uint64(comp.Len()), UncompressedSize64: uint64(n),
	})
	must(err)
	_, err = w.Write(comp.Bytes())
	must(err)
	must(zw.Close())
	return buf.Bytes()
}

// Overlap returns a zip whose central directory lists members entries that all
// point at the same local file (an overlapping-member bomb): the archive stays
// small while the sum of the declared members is members times inner bytes.
// Go's archive/zip reads every member; the names in the central directory
// differ from the single local header name (bomb.bin), which unzip or 7z may
// report. members is limited to 65535 and inner to 4 GiB (zip header fields).
func Overlap(members int, inner int64) []byte {
	if members < 1 || members > 0xFFFF || inner < 1 || inner >= 1<<32 {
		panic("zipfix: Overlap needs 1..65535 members and an inner size below 4 GiB")
	}
	base := Bomb(inner) // one honest member at offset 0
	r, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	must(err)
	f := r.File[0]
	off, err := f.DataOffset()
	must(err)
	local := base[:off+int64(f.CompressedSize64)] // local header + data of member 0
	var cd bytes.Buffer
	for i := 0; i < members; i++ {
		name := memberName(i)
		rec := make([]byte, 46)
		binary.LittleEndian.PutUint32(rec[0:], 0x02014b50)
		binary.LittleEndian.PutUint16(rec[4:], 20)
		binary.LittleEndian.PutUint16(rec[6:], 20)
		binary.LittleEndian.PutUint16(rec[10:], zip.Deflate)
		binary.LittleEndian.PutUint32(rec[16:], f.CRC32)
		binary.LittleEndian.PutUint32(rec[20:], uint32(f.CompressedSize64))
		binary.LittleEndian.PutUint32(rec[24:], uint32(f.UncompressedSize64))
		binary.LittleEndian.PutUint16(rec[28:], uint16(len(name)))
		binary.LittleEndian.PutUint32(rec[42:], 0) // local header offset: always member 0
		cd.Write(rec)
		cd.WriteString(name)
	}
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(end[8:], uint16(members))
	binary.LittleEndian.PutUint16(end[10:], uint16(members))
	binary.LittleEndian.PutUint32(end[12:], uint32(cd.Len()))
	binary.LittleEndian.PutUint32(end[16:], uint32(len(local)))
	return append(append(append([]byte(nil), local...), cd.Bytes()...), end...)
}

// EmptyArchive is a valid zip with no members: only the end record, which
// starts with PK 05 06 rather than PK 03 04 (case 43).
func EmptyArchive() []byte {
	var buf bytes.Buffer
	must(zip.NewWriter(&buf).Close())
	return buf.Bytes()
}

func deflate(data []byte) []byte {
	var b bytes.Buffer
	fw, err := flate.NewWriter(&b, flate.BestCompression)
	must(err)
	_, err = io.Copy(fw, bytes.NewReader(data))
	must(err)
	must(fw.Close())
	return b.Bytes()
}

func memberName(i int) string {
	const digits = "0123456789"
	b := []byte("m000000")
	for p, n := len(b)-1, i; p > 0; p, n = p-1, n/10 {
		b[p] = digits[n%10]
	}
	return string(b)
}

func must(err error) {
	if err != nil {
		panic("zipfix: " + err.Error())
	}
}
