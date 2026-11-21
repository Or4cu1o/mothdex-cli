package proxy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
		return StateCanceled, &Error{Code: CodeCanceled, Message: "canceled", Err: ctx.Err()}
	}
	link := j.job.Link

	u, err := CheckURL(link.URL, d.cfg.AllowedHosts) // on every attempt too: see transfer
	if err != nil {
		return StateFailed, asError(err)
	}
	j.url = u.String()

	if link.Size <= 0 || link.Size > d.cfg.MaxFileSize {
		return StateFailed, &Error{Code: CodeSizeMismatch, Message: "the size of the link is not acceptable"}
	}

	var nameOpts []NameOption
	nameOpts = append(nameOpts, WithFallback(j.job.Slug, link.Format, j.id))
	if d.cfg.AllowExecutable {
		nameOpts = append(nameOpts, AllowExecutable())
	}
	name, err := Sanitize(link.Filename, nameOpts...)
	switch {
	case errors.Is(err, ErrUnsafeName):
		return StateFailed, &Error{Code: CodeUnsafeName, Message: "the file has an executable extension", Err: err}
	case err != nil:
		return StateFailed, &Error{Code: CodeInternal, Message: "cannot name the file", Err: err}
	}
	j.name = name

	if !platformRe.MatchString(j.job.Platform) || !d.cfg.Platforms[j.job.Platform] {
		return StateFailed, &Error{Code: CodePathRejected, Message: "unknown platform id"}
	}
	rel, err := d.o.Confine(j.job.DestDir)
	if err != nil {
		return StateFailed, asError(err)
	}
	j.destRel = rel

	j.baseName, j.nameOpts = name, nameOpts
	if state, perr := d.reserveName(j, 0); state != "" || perr != nil {
		return state, perr
	}
	if perr := d.reserveSpace(j); perr != nil {
		return StateFailed, perr
	}
	j.staging = stagingDir + "/" + j.id + ".part"
	if jerr := d.o.Journal.Add(JournalEntry{JobID: j.id, Staging: j.staging, DestDir: j.destRel, Started: j.started}); jerr != nil {
		return StateFailed, &Error{Code: CodeInternal, Message: "cannot write the in-flight journal", Err: jerr}
	}
	j.journaled = true
	return "", nil
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
		if held && !exists {
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
		case d.cfg.OnExists == OnExistsOverwrite && held:
			if !info.Mode().IsRegular() {
				d.names.release(j.destRel, cand)
				return StateFailed, &Error{Code: CodePathRejected, Message: "the existing target is not a regular file"}
			}
			j.name, j.nameHeld, j.overwrite, j.renameN = cand, true, true, n
			return "", nil
		}
		if held {
			d.names.release(j.destRel, cand) // rename, or overwrite against another job of the batch
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

// reserveSpace is step 7: one claim per file system, all or nothing.
func (d *Downloader) reserveSpace(j *jobState) *Error {
	need, ok := spaceNeed(j.job.Link.Size)
	if !ok {
		return &Error{Code: CodeSizeMismatch, Message: "the size of the link is not acceptable"}
	}
	if err := d.o.Staging.MkdirAll(stagingDir, 0o700); err != nil {
		return &Error{Code: CodeInternal, Message: "cannot create the staging directory", Err: err}
	}
	free, dev, err := d.o.Staging.Free(stagingDir)
	if err != nil {
		return &Error{Code: CodeInternal, Message: "cannot read the free space of the staging directory", Err: err}
	}
	claims := []claim{{dev: dev, free: free, need: need}}
	dfree, ddev, err := d.o.Dest.Free(j.destRel)
	if err != nil {
		return &Error{Code: CodeInternal, Message: "cannot read the free space of the destination", Err: err}
	}
	// One object and one device: the staging file is renamed, no second copy exists.
	if d.o.Staging != d.o.Dest || ddev != dev {
		claims = append(claims, claim{dev: ddev, free: dfree, need: need})
	}
	if !d.ledger.reserve(claims) {
		return &Error{Code: CodeDiskFull, Message: "not enough free space for this download"}
	}
	j.claims = claims
	return nil
}

// openStaging creates the staging file: O_CREATE|O_EXCL|O_NOFOLLOW, 0600, name from the job id.
func (d *Downloader) openStaging(j *jobState) *Error {
	f, err := d.o.Staging.OpenExcl(j.staging)
	if err != nil {
		code := CodeInternal
		if isDiskFull(err) {
			code = CodeDiskFull
		}
		return &Error{Code: code, Message: "cannot create the staging file", Err: err}
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
