package proxy

import (
	"archive/zip"
	"context"
	"crypto/md5"  //nolint:gosec // upstream DATs publish md5 and sha1; this compares, it does not authenticate
	"crypto/sha1" //nolint:gosec // same reason
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Zip check caps (downloader.md section 4, TH-24).
const (
	zipMaxMembers   = 65535
	zipAbsoluteCap  = int64(64) << 30
	zipMinimumCap   = int64(1) << 30
	zipCapFactor    = 20
	verifyChunk     = 64 << 10
	statusOK        = "ok"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
	zipLocalHeader  = "PK\x03\x04"
	zipLocalHeaderN = len(zipLocalHeader)
)

func integrity(msg string, cause error) *attemptErr {
	return &attemptErr{kind: kindIntegrity, err: &Error{Code: CodeIntegrity, Message: msg, Err: cause}}
}

// structural is an integrity failure that a second download cannot fix (a bomb, too many
// members, a broken directory): fatal, so the hostile file is fetched once.
func structural(msg string, cause error) *attemptErr {
	return fatal(CodeIntegrity, msg, cause)
}

// verify runs the checks of section 4 on the staging file. The size was checked while
// streaming; the local SHA-256 was computed on the fly.
func (d *Downloader) verify(ctx context.Context, j *jobState) *attemptErr {
	d.event(j, StateVerifying, nil)
	j.crc, j.hash = statusSkipped, statusSkipped
	if d.cfg.SkipVerify {
		return nil
	}
	ra := &fileAt{f: j.file}
	isZip, err := hasZipMagic(ra)
	if err != nil {
		return fatal(CodeInternal, "cannot read the staging file", err)
	}
	var zr *zip.Reader
	if isZip {
		zr, err = zip.NewReader(ra, j.bytes)
		if err != nil {
			j.crc = statusFailed
			return structural("the zip archive is damaged", err) // the same file again would fail the same way
		}
		if ae := d.checkZip(ctx, j, zr); ae != nil {
			return ae
		}
		j.crc = statusOK
	}
	if h := j.job.Link.Hashes; h != nil {
		return d.checkHashes(ctx, j, ra, zr, h)
	}
	return nil
}

func hasZipMagic(ra io.ReaderAt) (bool, error) {
	var head [zipLocalHeaderN]byte
	n, err := ra.ReadAt(head[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return n == zipLocalHeaderN && string(head[:]) == zipLocalHeader, nil
}

// zipBudget is min(64 GiB, max(1 GiB, 20 x size)) without overflow.
func zipBudget(size int64) int64 {
	if size > zipAbsoluteCap/zipCapFactor {
		return zipAbsoluteCap
	}
	return min(zipAbsoluteCap, max(zipMinimumCap, size*zipCapFactor))
}

// checkZip reads every member to EOF, which makes archive/zip verify the CRC32. Nothing is
// extracted: the output goes to nowhere, and the work is bounded by the member and byte caps.
func (d *Downloader) checkZip(ctx context.Context, j *jobState, zr *zip.Reader) *attemptErr {
	if len(zr.File) > zipMaxMembers {
		j.crc = statusFailed
		return structural("the zip archive has too many members", nil)
	}
	budget := zipBudget(j.bytes)
	var total int64
	for _, f := range zr.File {
		if total += int64(min(f.UncompressedSize64, uint64(budget)+1)); total > budget { //nolint:gosec // clamped to the budget
			j.crc = statusFailed
			return structural("the zip archive declares too much data", nil)
		}
	}
	total = 0
	for _, f := range zr.File {
		n, err := drain(ctx, f, budget-total)
		total += n
		if ae := d.zipReadFailure(ctx, j, err); ae != nil {
			return ae
		}
	}
	return nil
}

// zipReadFailure turns a member read error into the right outcome: cancellation of the job is
// not an integrity failure.
func (d *Downloader) zipReadFailure(ctx context.Context, j *jobState, err error) *attemptErr {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return &attemptErr{kind: kindFatal, err: canceled(ctx)}
	case errors.Is(err, errZipTooBig):
		j.crc = statusFailed
		return structural("the zip archive inflates past the cap", err)
	default:
		j.crc = statusFailed
		return integrity("the zip archive failed its checks", err) // a bad CRC may be a bad transfer: one retry
	}
}

var errZipTooBig = errors.New("proxy: zip members exceed the byte cap")

// drain reads a member to EOF into nowhere, stopping at limit bytes and on ctx.
func drain(ctx context.Context, f *zip.File, limit int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	n, err := copyCtx(ctx, io.Discard, rc, limit)
	return n, err
}

// copyCtx copies at most limit bytes, checking ctx between chunks; it fails with errZipTooBig
// if the source still has data at the limit.
func copyCtx(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, verifyChunk)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, errZipTooBig
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
		}
		if errors.Is(err, io.EOF) {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// checkHashes compares against the upstream hash with the strongest algorithm available
// (sha1, md5, crc32). scope=file hashes the staging file; scope=content hashes the only member
// of a zip, or the file itself when it is not an archive; several members: skipped.
func (d *Downloader) checkHashes(ctx context.Context, j *jobState, ra *fileAt, zr *zip.Reader, h *mothdex.Hashes) *attemptErr {
	alg, want := pickHash(h)
	if alg == nil {
		return nil
	}
	var src io.ReadCloser
	switch {
	case h.Scope == mothdex.HashScopeContent && zr != nil:
		if len(zr.File) != 1 {
			return nil // several members: skipped
		}
		rc, err := zr.File[0].Open()
		if err != nil {
			j.hash = statusFailed
			return integrity("cannot read the archive member", err)
		}
		src = rc
	default:
		src = io.NopCloser(io.NewSectionReader(ra, 0, j.bytes))
	}
	defer func() { _ = src.Close() }()
	limit := j.bytes
	if h.Scope == mothdex.HashScopeContent && zr != nil {
		limit = zipBudget(j.bytes) // a member inflates; the file itself is exactly j.bytes
	}
	hh := alg()
	if _, err := copyCtx(ctx, hh, src, limit); err != nil {
		if ctx.Err() != nil {
			return &attemptErr{kind: kindFatal, err: canceled(ctx)}
		}
		j.hash = statusFailed
		return integrity("cannot hash the file", err)
	}
	if hex.EncodeToString(hh.Sum(nil)) != want {
		j.hash = statusFailed
		return integrity("the file does not match the upstream hash", nil)
	}
	j.hash = statusOK
	return nil
}

// pickHash returns the constructor and the expected lower-case hex of the strongest digest.
func pickHash(h *mothdex.Hashes) (func() hash.Hash, string) {
	switch {
	case h.SHA1 != "":
		return sha1.New, h.SHA1 //nolint:gosec // see the import
	case h.MD5 != "":
		return md5.New, h.MD5 //nolint:gosec // see the import
	case h.CRC32 != "":
		return func() hash.Hash { return crc32.NewIEEE() }, h.CRC32
	}
	return nil, ""
}

// fileAt makes a seekable File an io.ReaderAt for archive/zip; reads are serialized.
type fileAt struct {
	mu sync.Mutex
	f  File
}

var _ io.ReaderAt = (*fileAt)(nil)

func (a *fileAt) ReadAt(p []byte, off int64) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.f.Seek(off, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek: %w", err)
	}
	n, err := io.ReadFull(a.f, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}
