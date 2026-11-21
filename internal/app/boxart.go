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

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

// boxartExt maps the content types the cover fetcher accepts (cache.md section 4) to extensions.
var boxartExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// SaveBoxart writes the entry's cover next to where its ROM goes (destDir "" = the same rule as a
// download) and returns the file path. The write goes through the confined destination root
// (O_EXCL, no links followed, launcher folders refused), so a hostile directory layout cannot
// redirect it. An existing file is kept and its path returned; the cover is never replaced.
// Entries without a usable cover give not_found.
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
	name, nerr := proxy.Sanitize(firstNonEmpty(e.Title, e.Slug)+ext, proxy.WithFallback(e.Slug, ext[1:], "boxart"))
	if nerr != nil {
		return "", &Error{Code: string(proxy.CodeUnsafeName), Message: "the cover file name is not safe", Cause: nerr}
	}
	if mk {
		if merr := os.MkdirAll(root, 0o700); merr != nil {
			return "", &Error{Code: CodePermissionDenied, Message: "cannot create the output directory", Cause: merr}
		}
	}
	rfs, err := proxy.OpenRootFS(root)
	if err != nil {
		return "", &Error{Code: CodeInvalidRequest, Message: "cannot open the destination root", Cause: err}
	}
	defer func() { _ = rfs.Close() }()
	rel, err := d.confiner(rfs).Resolve(dest)
	if err != nil {
		return "", wrap(err)
	}
	target := path.Join(rel, name)
	final := filepath.Join(rfs.Dir(), filepath.FromSlash(target))
	f, err := rfs.OpenExcl(target)
	if errors.Is(err, fs.ErrExist) {
		return final, nil
	}
	if err != nil {
		return "", &Error{Code: CodePermissionDenied, Message: "cannot write the cover", Cause: err}
	}
	if _, err = io.Copy(f, io.LimitReader(img, 8<<20)); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = rfs.Remove(target)
		return "", &Error{Code: CodePermissionDenied, Message: "cannot write the cover", Cause: err}
	}
	return final, nil
}
