package proxy

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"time"
)

// orphanAge is how old an unfinished file must be, by mtime, before startup cleanup may remove
// it: a live job writes at least every idle_timeout, far below this.
const orphanAge = 24 * time.Hour

var (
	partRe  = regexp.MustCompile(`^[a-z2-7]{26}\.part$`)
	jobIDRe = regexp.MustCompile(`^[a-z2-7]{26}$`)
)

// Cleanup removes what crashed jobs left behind (downloader.md 6.4, TH-15): only regular files
// (never followed, never recursed) older than 24 h whose names match exactly, in the staging
// directory and, through the in-flight journal, as .<jobID>.mothdex-tmp in destination
// directories. Journal entries whose files are gone are pruned. It never touches a job that
// runs in this process.
func (d *Downloader) Cleanup(ctx context.Context) error {
	now := d.o.Clock.Now()
	errs, err := d.cleanStaging(ctx, now)
	if err != nil {
		return err
	}
	jerrs, err := d.cleanJournaled(ctx, now)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return errors.Join(append(errs, err)...) // keep the staging errors when the journal fails
	}
	return errors.Join(append(errs, jerrs...)...)
}

// cleanStaging removes old <jobID>.part files from the staging directory.
func (d *Downloader) cleanStaging(ctx context.Context, now time.Time) ([]error, error) {
	var errs []error
	entries, err := d.o.Staging.ReadDir(stagingDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name := e.Name()
		if !partRe.MatchString(name) || d.isLive(name[:26]) {
			continue
		}
		if _, err := removeIfOld(d.o.Staging, stagingDir+"/"+name, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errs, nil
}

// cleanJournaled removes the leftovers named by the in-flight journal (staged in place or the
// cross-device copy) and prunes the entries whose files are gone.
func (d *Downloader) cleanJournaled(ctx context.Context, now time.Time) ([]error, error) {
	journal, err := d.o.Journal.Entries()
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, je := range journal {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !jobIDRe.MatchString(je.JobID) || d.isLive(je.JobID) {
			continue // a malformed entry is never trusted; a live job keeps its entry
		}
		stagingGone, err1 := removeIfOld(d.o.Staging, stagingDir+"/"+je.JobID+".part", now)
		destGone, err2 := d.cleanDestLeftovers(je, now)
		errs = append(errs, err1, err2)
		if stagingGone && destGone {
			if err := d.o.Journal.Remove(je.JobID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs, nil
}

// cleanDestLeftovers removes .<jobID>.part and .<jobID>.mothdex-tmp in the entry's destination.
func (d *Downloader) cleanDestLeftovers(je JournalEntry, now time.Time) (bool, error) {
	dir := path.Clean(je.DestDir)
	if !fs.ValidPath(dir) {
		return true, nil
	}
	gone := true
	var errs error
	for _, suffix := range []string{".part", ".mothdex-tmp"} {
		g, err := removeIfOld(d.o.Dest, joinName(dir, "."+je.JobID+suffix), now)
		gone = gone && g
		errs = errors.Join(errs, err)
	}
	return gone, errs
}

func (d *Downloader) isLive(id string) bool {
	d.liveMu.Lock()
	defer d.liveMu.Unlock()
	return d.live[id]
}

// removeIfOld removes a regular file older than orphanAge. gone reports that the name no
// longer exists or was removed; anything else (young, a directory, a link) stays untouched.
func removeIfOld(fsys FS, name string, now time.Time) (gone bool, err error) {
	info, err := fsys.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, err
	case !info.Mode().IsRegular() || now.Sub(info.ModTime()) <= orphanAge:
		return false, nil
	}
	if err := fsys.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}
