package proxy

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"syscall"
)

// finalize is the Finalizing state (downloader.md 3 and 6.3): fsync the file, place it under
// its final name without ever replacing an existing file, fsync the directory, and only then
// report Done (CR-5).
func (d *Downloader) finalize(ctx context.Context, j *jobState) (State, *Error) {
	d.event(j, StateFinalizing, nil)
	if err := j.file.Sync(); err != nil {
		return StateFailed, writeFailure("cannot flush the staging file", err)
	}
	if err := d.checkStagingName(j); err != nil {
		return StateFailed, err
	}
	src := j.staging
	if !j.stageInDest { // staged on another device: copy into the destination, then rename there
		// The copy reads the descriptor that was verified, never the name (T-17 audit m2).
		var err *Error
		if src, err = d.copyToDest(ctx, j, j.file); err != nil {
			return stateOf(err), err
		}
	}
	info, _ := j.file.Stat()
	cerr := j.file.Close()
	j.file = nil
	if cerr != nil {
		return StateFailed, writeFailure("cannot close the staging file", cerr)
	}
	if !j.stageInDest {
		_ = d.o.Staging.Remove(j.staging) // after the close: Windows cannot remove an open file
		j.staging = ""
	}
	for {
		if ctx.Err() != nil {
			return StateCanceled, canceled(ctx)
		}
		final := joinName(j.destRel, j.name)
		err := d.place(j, src, final)
		switch {
		case err == nil:
			return d.finishPlacement(j)
		case errors.Is(err, syscall.EXDEV) && src != j.tmp:
			rd, rerr := d.reopenVerified(j, info)
			if rerr != nil {
				return StateFailed, rerr
			}
			var cerr *Error
			src, cerr = d.copyToDest(ctx, j, rd)
			_ = rd.Close()
			if cerr != nil {
				return stateOf(cerr), cerr
			}
			_ = d.stagingFS(j).Remove(j.staging)
		case errors.Is(err, fs.ErrExist) && !j.overwrite:
			state, perr := d.existsAtFinalize(j)
			if state != "" || perr != nil {
				return orFailed(state, perr), perr
			}
		case errors.As(err, new(*notRegularError)):
			return StateFailed, &Error{Code: CodePathRejected, Message: "the existing target is not a regular file"}
		default:
			return StateFailed, writeFailure("cannot place the file", err)
		}
	}
}

// checkStagingName requires that the staging name still refers to the file the downloader
// has been writing: same file, regular, not a link planted in its place (T-17 review).
func (d *Downloader) checkStagingName(j *jobState) *Error {
	fi, err1 := j.file.Stat()
	li, err2 := d.stagingFS(j).Lstat(j.staging)
	if err1 != nil || err2 != nil || !li.Mode().IsRegular() || !os.SameFile(fi, li) {
		return &Error{Code: CodePathRejected, Message: "the staging file was replaced while downloading"}
	}
	return nil
}

// checkPlaced requires the final name to be a regular file of the expected size, and removes
// what it placed if not.
func (d *Downloader) checkPlaced(j *jobState) *Error {
	final := joinName(j.destRel, j.name)
	info, err := d.o.Dest.Lstat(final)
	if err == nil && info.Mode().IsRegular() && info.Size() == j.bytes {
		return nil
	}
	_ = d.o.Dest.Remove(final)
	return &Error{Code: CodePathRejected, Message: "the placed file is not the downloaded file"}
}

// writeFailure classifies a write or rename error.
func writeFailure(msg string, err error) *Error {
	code := CodeInternal
	if isDiskFull(err) {
		code = CodeDiskFull
	}
	return &Error{Code: code, Message: msg, Err: err}
}

// finishPlacement is the last step after the rename: flush the directory.
func (d *Downloader) finishPlacement(j *jobState) (State, *Error) {
	j.tmp = "" // the copy became the final file
	if err := d.checkPlaced(j); err != nil {
		return StateFailed, err
	}
	d.names.markFinal(j.destRel, j.name)
	// If the directory flush fails below the file is already in place; the job is reported
	// Failed (Done needs both fsyncs, CR-5) and the file is left for the user to see.
	if err := d.o.Dest.SyncDir(j.destRel); err != nil {
		return StateFailed, writeFailure("cannot flush the destination directory", err)
	}
	return StateDone, nil
}

// place puts src under final: the plain no-replace chain, or the one replacing rename of
// --force after the target was inspected.
func (d *Downloader) place(j *jobState, src, final string) error {
	if !j.overwrite {
		return d.noReplace(src, final)
	}
	info, err := d.o.Dest.Lstat(final)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return d.noReplace(src, final) // it vanished: a plain placement is enough
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return &notRegularError{}
	}
	return d.o.Finalizer.Rename(d.o.Dest, src, final, true)
}

type notRegularError struct{}

func (*notRegularError) Error() string { return "the existing target is not a regular file" }

// noReplace is Finalizer.Rename without replace, with the fallback chain for file systems that
// cannot do it: a hard link then removing the source, then an O_EXCL placeholder that the
// final rename replaces (it is ours).
func (d *Downloader) noReplace(src, final string) error {
	err := d.o.Finalizer.Rename(d.o.Dest, src, final, false)
	if !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	err = d.o.Dest.Link(src, final)
	if err == nil {
		_ = d.o.Dest.Remove(src) // a leftover link of the same file is harmless and cleaned later
		return nil
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	f, err := d.o.Dest.OpenExcl(final)
	if err != nil {
		return err
	}
	mine, _ := f.Stat()
	if err := f.Close(); err != nil {
		d.removeIfMine(final, mine)
		return err
	}
	// Replace only our own placeholder: if the name was swapped meanwhile, leave it alone.
	if !d.isMine(final, mine) {
		return fs.ErrExist
	}
	if err := d.o.Finalizer.Rename(d.o.Dest, src, final, true); err != nil {
		d.removeIfMine(final, mine)
		return err
	}
	return nil
}

// isMine reports whether name still is the file we created (same file, regular).
func (d *Downloader) isMine(name string, mine fs.FileInfo) bool {
	if mine == nil {
		return false
	}
	cur, err := d.o.Dest.Lstat(name)
	return err == nil && cur.Mode().IsRegular() && os.SameFile(mine, cur)
}

// removeIfMine removes the placeholder we created, and nothing else.
func (d *Downloader) removeIfMine(name string, mine fs.FileInfo) {
	if d.isMine(name, mine) {
		_ = d.o.Dest.Remove(name)
	}
}

// existsAtFinalize applies on_exists again when the final name appeared after Preflight
// (another process, T-16 case 21). It returns a terminal state, or nothing to try again with
// the possibly new name.
func (d *Downloader) existsAtFinalize(j *jobState) (State, *Error) {
	switch d.cfg.OnExists {
	case OnExistsRename:
		d.names.release(j.destRel, j.name)
		j.nameHeld = false
		return d.reserveName(j, j.renameN+1)
	case OnExistsOverwrite:
		if d.names.isFinal(j.destRel, j.name) { // a file of this batch: never replace it, rename
			d.names.release(j.destRel, j.name)
			j.nameHeld = false
			return d.reserveName(j, j.renameN+1)
		}
		info, err := d.o.Dest.Lstat(joinName(j.destRel, j.name))
		if err == nil && !info.Mode().IsRegular() {
			return StateFailed, &Error{Code: CodePathRejected, Message: "the existing target is not a regular file"}
		}
		j.overwrite = true
		return "", nil
	default: // skip
		return StateSkipped, nil
	}
}

// reopenVerified opens the staging name again and requires it to be the very file that was
// verified (same file as the descriptor that was closed), for the rare EXDEV fallback on one
// device. A swapped name is refused.
func (d *Downloader) reopenVerified(j *jobState, want fs.FileInfo) (io.ReadCloser, *Error) {
	rd, err := d.stagingFS(j).Open(j.staging)
	if err != nil {
		return nil, &Error{Code: CodeInternal, Message: "cannot reopen the staging file", Err: err}
	}
	st, ok := rd.(interface{ Stat() (fs.FileInfo, error) })
	if !ok || want == nil {
		_ = rd.Close()
		return nil, &Error{Code: CodePathRejected, Message: "cannot verify the staging file"}
	}
	got, err := st.Stat()
	if err != nil || !os.SameFile(want, got) {
		_ = rd.Close()
		return nil, &Error{Code: CodePathRejected, Message: "the staging file was replaced before it was placed"}
	}
	return rd, nil
}

// copyToDest copies the verified file into <destdir>/.<jobID>.mothdex-tmp (O_EXCL, 0600) and
// flushes it. The copy is what the final rename moves, so a crash leaves only the hidden
// temporary name, never a partial final file. src is rewound first and must yield exactly the
// bytes that were downloaded.
func (d *Downloader) copyToDest(ctx context.Context, j *jobState, src io.Reader) (string, *Error) {
	if sk, ok := src.(io.Seeker); ok {
		if _, err := sk.Seek(0, io.SeekStart); err != nil {
			return "", &Error{Code: CodeInternal, Message: "cannot rewind the staging file", Err: err}
		}
	}
	tmp := joinName(j.destRel, "."+j.id+".mothdex-tmp")
	dst, err := d.o.Dest.OpenExcl(tmp)
	if err != nil {
		return "", writeFailure("cannot create the temporary copy", err)
	}
	j.tmp = tmp
	n, err := copyCtx(ctx, dst, src, math.MaxInt64)
	if err == nil && n != j.bytes {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		_ = dst.Close()
		if ctx.Err() != nil {
			return "", canceled(ctx)
		}
		return "", writeFailure("cannot copy the file to the destination", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return "", writeFailure("cannot flush the temporary copy", err)
	}
	if err := dst.Close(); err != nil {
		return "", writeFailure("cannot close the temporary copy", err)
	}
	return tmp, nil
}
