package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// boxartExt maps the content types the cover fetcher accepts (cache.md section 4) to extensions.
var boxartExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// SaveBoxart writes the entry's cover for the standalone `boxart` command: <title>.<ext> in
// destDir ("" = the same rule as a download). See saveCover for the write rules.
func (s *Service) SaveBoxart(ctx context.Context, ref Ref, destDir string) (string, error) {
	d := s.dl
	if d == nil {
		return "", newError(CodeInternal, "downloads are not enabled")
	}
	entry, err := s.Entry(ctx, ref)
	if err != nil {
		return "", err
	}
	e := entry.Value
	root, dest, mk, err := d.destination(destDir, e.Platform)
	if err != nil {
		return "", err
	}
	return s.saveCover(ctx, e, root, dest, mk, func(ext string) string { return firstNonEmpty(e.Title, e.Slug) + ext })
}

// SaveJobBoxart writes the cover of a finished download next to its file, named after the file
// (<stem of the final name>.<ext>) so a frontend pairs them by stem. destDir is the -o the job was
// queued with ("" for the profile rule). Only a Done job has a cover to save.
func (s *Service) SaveJobBoxart(ctx context.Context, job JobSnapshot, destDir string) (string, error) {
	d := s.dl
	if d == nil {
		return "", newError(CodeInternal, "downloads are not enabled")
	}
	if job.State != StateDone || job.Path == "" {
		return "", newError(CodeInvalidRequest, "only a finished download has a cover to save")
	}
	entry, err := s.Entry(ctx, Ref{Slug: job.Slug})
	if err != nil {
		return "", err
	}
	root, _, mk, err := d.destination(destDir, entry.Value.Platform)
	if err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(filepath.Base(job.Path), filepath.Ext(job.Path))
	return s.saveCover(ctx, entry.Value, root, filepath.Dir(job.Path), mk, func(ext string) string { return stem + ext })
}

// saveCover writes the entry's cover into dest through the confined root: the cover comes from the
// validated fetcher and cache, the extension from an allowlist of content types, the name from
// proxy.Sanitize, the write is O_EXCL through its own RootFS with the downloader's Confiner
// (launcher folders refused), and an existing file is kept, never replaced. The file is staged under
// a temporary name and published with a link, so a crash never leaves a truncated cover.
func (s *Service) saveCover(ctx context.Context, e mothdex.Entry, root, dest string, mkRoot bool, nameFor func(ext string) string) (string, error) {
	d := s.dl
	img, ct, err := s.Cover(ctx, Ref{Slug: e.Slug})
	if err != nil {
		return "", err
	}
	if img == nil {
		return "", newError(CodeNotFound, "the entry has no usable cover")
	}
	defer func() { _ = img.Close() }()
	mt, _, _ := mime.ParseMediaType(ct)
	ext, ok := boxartExt[mt]
	if !ok {
		return "", newError(CodeNotFound, "the cover has an unsupported type")
	}
	name, nerr := proxy.Sanitize(nameFor(ext), proxy.WithFallback(e.Slug, ext[1:], "boxart"))
	if nerr != nil {
		return "", &Error{Code: string(proxy.CodeUnsafeName), Message: "the cover file name is not safe", Cause: nerr}
	}
	created := false
	if mkRoot {
		if _, serr := os.Stat(root); errors.Is(serr, fs.ErrNotExist) {
			created = true
		}
		if merr := os.MkdirAll(root, 0o700); merr != nil {
			return "", &Error{Code: CodePermissionDenied, Message: "cannot create the output directory", Cause: merr}
		}
	}
	final, err := d.writeCover(root, dest, name, img)
	if err != nil && created {
		_ = os.Remove(root) // a refused destination must not leave the directory this call made
	}
	return final, err
}

func (d *downloads) writeCover(root, dest, name string, img io.Reader) (string, error) {
	rfs, err := proxy.OpenRootFS(root)
	if err != nil {
		return "", &Error{Code: CodePermissionDenied, Message: "cannot open the destination root", Cause: err}
	}
	defer func() { _ = rfs.Close() }()
	rel, err := d.confiner(rfs).Resolve(dest)
	if err != nil {
		return "", wrap(err)
	}
	target := path.Join(rel, name)
	final := filepath.Join(rfs.Dir(), filepath.FromSlash(target))
	if _, lerr := rfs.Lstat(target); lerr == nil {
		return final, nil // never replaced
	}
	part := path.Join(rel, "."+name+".part")
	_ = rfs.Remove(part) // a leftover of a crash
	f, err := rfs.OpenExcl(part)
	if err != nil {
		return "", &Error{Code: CodePermissionDenied, Message: "cannot write the cover", Cause: err}
	}
	if _, err = io.Copy(f, io.LimitReader(img, 8<<20)); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = rfs.Link(part, target)
		switch {
		case errors.Is(err, fs.ErrExist):
			err = nil // another writer published first: keep theirs
		case err != nil: // no hard links (FAT, exFAT cards): copy under O_EXCL instead
			err = copyExcl(rfs, part, target)
		}
	}
	_ = rfs.Remove(part)
	if err != nil {
		return "", &Error{Code: CodePermissionDenied, Message: "cannot write the cover", Cause: err}
	}
	return final, nil
}

// copyExcl is the fallback publish: a new file under O_EXCL filled from the staged one. A failure
// removes the half-written target.
func copyExcl(rfs *proxy.RootFS, from, to string) error {
	src, err := rfs.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := rfs.OpenExcl(to)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = rfs.Remove(to)
	}
	return err
}
