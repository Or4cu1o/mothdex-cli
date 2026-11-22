package proxy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

const (
	maxRenameTries = 99
	stagingDir     = "downloads"
)

var platformRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// preflight runs the checks of downloader.md 3.1 in order. It returns a terminal state with a
// nil error for a skipped job, or an error for a failed or canceled one.
func (d *Downloader) preflight(ctx context.Context, j *jobState) (State, *Error) {
	d.event(j, StatePreflight, nil)
	if ctx.Err() != nil {
		return StateCanceled, canceled(ctx)
	}
	if err := d.checkLink(j); err != nil {
		return StateFailed, err
	}
	if state, perr := d.reserveName(j, 0); state != "" || perr != nil {
		return state, perr
	}
	if perr := d.reserveSpace(j); perr != nil {
		return StateFailed, perr
	}
	if jerr := d.o.Journal.Add(JournalEntry{JobID: j.id, Staging: j.staging, DestDir: j.destRel, Root: d.o.DestRoot, Started: j.started}); jerr != nil {
		return StateFailed, &Error{Code: CodeInternal, Message: "cannot write the in-flight journal", Err: jerr}
	}
	j.journaled = true
	return "", nil
}

// checkLink validates what came from the API or the cache: URL, size, name, platform and
// destination directory (steps 1 to 4).
func (d *Downloader) checkLink(j *jobState) *Error {
	link := j.job.Link
	u, err := CheckURL(link.URL, d.cfg.AllowedHosts) // on every attempt too: see transfer
	if err != nil {
		return asError(err)
	}
	j.url = u.String()
	if link.Size <= 0 || link.Size > d.cfg.MaxFileSize {
		return &Error{Code: CodeSizeMismatch, Message: "the size of the link is not acceptable"}
	}
	opts := []NameOption{WithFallback(j.job.Slug, link.Format, j.id)}
	if d.cfg.AllowExecutable {
		opts = append(opts, AllowExecutable())
	}
	name, err := Sanitize(link.Filename, opts...)
	switch {
	case errors.Is(err, ErrUnsafeName):
		return &Error{Code: CodeUnsafeName, Message: "the file has an executable extension", Err: err}
	case err != nil:
		return &Error{Code: CodeInternal, Message: "cannot name the file", Err: err}
	}
	j.name, j.baseName, j.nameOpts = name, name, opts
	if !platformRe.MatchString(j.job.Platform) || !d.cfg.Platforms[j.job.Platform] {
		return &Error{Code: CodePathRejected, Message: "unknown platform id"}
	}
	rel, err := d.o.Confine(j.job.DestDir)
	if err != nil {
		return asError(err)
	}
	j.destRel = rel
	return nil
}

// reserveName takes the in-process name reservation and applies on_exists against the batch
// and the file system (steps 5 and 6).
func (d *Downloader) reserveName(j *jobState, start int) (State, *Error) {
	base := j.baseName
	for n := start; n <= maxRenameTries; n++ {
		cand := base
		if n > 0 {
			var err error
			if cand, err = numbered(base, n, j.nameOpts); err != nil {
				return StateFailed, &Error{Code: CodeExists, Message: "cannot build a numbered name", Err: err}
			}
		}
		held := d.names.take(j.destRel, cand, j.id)
		exists, info, err := d.stat(joinName(j.destRel, cand))
		if err != nil {
			if held {
				d.names.release(j.destRel, cand)
			}
			return StateFailed, err
		}
		// A name finalized earlier by this Downloader counts as existing even when the file system
		// does not see it (case-insensitive spelling on a case-sensitive disk).
		if held && !exists && !d.names.isFinal(j.destRel, cand) {
			j.name, j.nameHeld, j.renameN = cand, true, n
			return "", nil
		}
		switch {
		case d.cfg.OnExists == OnExistsSkip:
			if held {
				d.names.release(j.destRel, cand)
			}
			j.name = cand
			return StateSkipped, nil
		case d.cfg.OnExists == OnExistsOverwrite && held && !d.names.isFinal(j.destRel, cand):
			if !info.Mode().IsRegular() {
				d.names.release(j.destRel, cand)
				return StateFailed, &Error{Code: CodePathRejected, Message: "the existing target is not a regular file"}
			}
			j.name, j.nameHeld, j.overwrite, j.renameN = cand, true, true, n
			return "", nil
		}
		if held {
			d.names.release(j.destRel, cand) // rename; or overwrite against a name of this batch
		}
	}
	return StateFailed, &Error{Code: CodeExists, Message: "all numbered names up to (99) are taken"}
}

// stat is Lstat on a final name: exists is false for a missing name, an error for anything
// unexpected.
func (d *Downloader) stat(name string) (exists bool, info fs.FileInfo, perr *Error) {
	info, err := d.o.Dest.Lstat(name)
	switch {
	case err == nil:
		return true, info, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil, nil
	default:
		return false, nil, &Error{Code: CodePathRejected, Message: "cannot inspect the destination", Err: err}
	}
}

// numbered turns "name.ext" into "name (n).ext" and sanitizes the result again, so the length
// cap and the deny-list still hold.
func numbered(name string, n int, opts []NameOption) (string, error) {
	stem, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		stem, ext = name[:i], name[i:]
	}
	return Sanitize(fmt.Sprintf("%s (%d)%s", stem, n, ext), opts...)
}

// reserveSpace is step 7: one claim per file system, all or nothing. When the staging
// directory and the destination are on the same device the file is staged inside the
// destination (written once, plain rename) and only one claim is made; otherwise it is staged
// in paths.temp, copied later, and both file systems are claimed (T-17-arch M2). The estimate
// is link.size here; the claim is swapped for Content-Length after the headers (3.2 step 5).
func (d *Downloader) reserveSpace(j *jobState) *Error {
	claims, sameDev, perr := d.spaceClaims(j, j.job.Link.Size)
	if perr != nil {
		return perr
	}
	j.stageInDest = sameDev
	if j.stageInDest {
		j.staging = joinName(j.destRel, "."+j.id+".part")
	} else {
		j.staging = stagingDir + "/" + j.id + ".part"
	}
	if !d.ledger.reserve(claims) {
		return &Error{Code: CodeDiskFull, Message: "not enough free space for this download"}
	}
	j.claims = claims
	return nil
}

// spaceClaims measures the free space and builds the claims for a file of size bytes. sameDev
// reports that the staging directory and the destination share a device (one claim).
func (d *Downloader) spaceClaims(j *jobState, size int64) (claims []claim, sameDev bool, perr *Error) {
	need, ok := spaceNeed(size)
	if !ok {
		return nil, false, &Error{Code: CodeSizeMismatch, Message: "the size of the link is not acceptable"}
	}
	if err := d.o.Staging.MkdirAll(stagingDir, 0o700); err != nil {
		return nil, false, &Error{Code: CodeInternal, Message: "cannot create the staging directory", Err: err}
	}
	free, dev, err := d.o.Staging.Free(stagingDir)
	if err != nil {
		return nil, false, &Error{Code: CodeInternal, Message: "cannot read the free space of the staging directory", Err: err}
	}
	dfree, ddev, err := d.o.Dest.Free(d.existingDir(j.destRel))
	if err != nil {
		return nil, false, &Error{Code: CodeInternal, Message: "cannot read the free space of the destination", Err: err}
	}
	claims = []claim{{dev: ddev, free: dfree, need: need}}
	if dev != ddev {
		claims = append(claims, claim{dev: dev, free: free, need: need})
	}
	return claims, dev == ddev, nil
}

// existingDir is the nearest existing ancestor of a destination directory that may not exist
// yet (it is created only when the staging file is).
func (d *Downloader) existingDir(rel string) string {
	for p := rel; p != "." && p != "/" && p != ""; p = path.Dir(p) {
		if _, err := d.o.Dest.Lstat(p); err == nil {
			return p
		}
	}
	return "."
}

// openStaging creates the destination directory (0700) and the staging file with
// O_CREATE|O_EXCL|O_NOFOLLOW, 0600, named from the job id.
func (d *Downloader) openStaging(j *jobState) *Error {
	if err := d.o.Dest.MkdirAll(j.destRel, 0o700); err != nil {
		return &Error{Code: CodePathRejected, Message: "cannot create the destination directory", Err: err}
	}
	f, err := d.stagingFS(j).OpenExcl(j.staging)
	if err != nil {
		return writeFailure("cannot create the staging file", err)
	}
	j.file = f
	return nil
}

// asError turns any error into a *Error, keeping a typed one as it is.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: "unexpected error", Err: err}
}
