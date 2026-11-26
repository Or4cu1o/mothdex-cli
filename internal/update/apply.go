package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// Progress runs serially on the Apply goroutine. Total is zero when unknown.
type Progress struct {
	Step        string
	Done, Total int64
}

// Staged contains verified notes and the origin verification result, before any swap.
type Staged struct {
	From, To         string
	Downgrade        bool
	Signature, Notes string
}

// ApplyOptions controls explicit updates. Confirm must observe the caller's ctx while showing a dialog.
type ApplyOptions struct {
	Version        string
	AllowDowngrade bool
	Progress       func(Progress)
	Confirm        func(Staged) bool
}

// Result describes an applied update, including the restart hint for persistent interfaces.
type Result struct {
	From, To, Signature                 string
	GUI, AliasRelinked, RestartRequired bool
	Notes                               string
}

func canceled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return failure(app.CodeCanceled, "canceled", err)
	}
	return nil
}
func step(ctx context.Context, o ApplyOptions, name string) error {
	if err := canceled(ctx); err != nil {
		return err
	}
	if o.Progress != nil {
		o.Progress(Progress{Step: name})
	}
	return canceled(ctx)
}

// Apply verifies all components before confirmation, then commits or rolls back as one transaction.
func (u *Updater) Apply(ctx context.Context, o ApplyOptions) (result Result, err error) {
	result.From = u.o.Current
	committing := false
	defer func() {
		if err != nil && ctx.Err() != nil && !committing {
			err = failure(app.CodeCanceled, "canceled", ctx.Err())
		}
		status := "current"
		if result.RestartRequired {
			status = "updated"
		}
		reason := ""
		restored := false
		if err != nil {
			status = "failed"
			var ae *app.Error
			if errors.As(err, &ae) {
				reason, _ = ae.Args["reason"].(string)
				restored, _ = ae.Args["restored"].(bool)
			}
		}
		u.o.Log.Info("update apply", "from", result.From, "to", result.To, "signature", result.Signature, "result", status, "reason", reason, "restored", restored)
	}()
	if !u.mu.TryLock() {
		return result, failure(app.CodeUpdateFailed, "in_progress", nil)
	}
	defer u.mu.Unlock()
	s, err := u.Managed()
	if err != nil {
		return result, err
	}
	dir := filepath.Dir(u.o.Exe)
	unlock, err := acquireLock(dir)
	if err != nil {
		return result, failure(app.CodeUpdateFailed, "in_progress", err)
	}
	defer unlock()
	if err = step(ctx, o, "discover"); err != nil {
		return result, err
	}
	version := o.Version
	if version == "" {
		r, e := u.Latest(ctx)
		if e != nil {
			return result, e
		}
		version = r.Version
		if compare(version, u.o.Current) <= 0 {
			result.To = u.o.Current
			return result, nil
		}
	}
	if !validVersion(version) {
		return result, failure(app.CodeUsage, "version", nil)
	}
	downgrade := compare(version, u.o.Current) < 0
	if downgrade && !o.AllowDowngrade {
		return result, failure(app.CodeUsage, "downgrade", nil)
	}
	result.To = version
	result.GUI = s.GUI
	stageDir, err := os.MkdirTemp(dir, ".mothdex-update-")
	if err != nil {
		return result, failure(app.CodeUpdateFailed, "swap_failed", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()
	release, err := u.stage(ctx, o, s, stageDir, version)
	if err != nil {
		return result, err
	}
	result.Signature = release.signature
	result.Notes = release.notes
	if err = canceled(ctx); err != nil {
		return result, err
	}
	if o.Confirm == nil {
		return result, failure(app.CodeConfirmationNeeded, "confirmation", nil)
	}
	confirmed := o.Confirm(Staged{From: u.o.Current, To: version, Downgrade: downgrade, Signature: release.signature, Notes: release.notes})
	if err = canceled(ctx); err != nil {
		return result, err
	}
	if !confirmed {
		return result, failure(app.CodeCanceled, "canceled", nil)
	}
	if err = step(ctx, o, "swap"); err != nil {
		return result, err
	}
	// Once swapping begins, cancellation cannot leave a partially updated installation.
	committing = true
	if err = u.swap(context.WithoutCancel(ctx), o, s, stageDir, release); err != nil {
		return result, err
	}
	result.AliasRelinked = runtime.GOOS == "windows" && s.Alias != ""
	result.RestartRequired = true
	if o.Progress != nil {
		o.Progress(Progress{Step: "done"})
	}
	return result, nil
}

type stagedRelease struct {
	version, signature, notes string
	files                     []string
	notices                   map[string][]byte
}

func (u *Updater) stage(ctx context.Context, o ApplyOptions, s State, dir, version string) (stagedRelease, error) {
	r := stagedRelease{version: version}
	if err := step(ctx, o, "download"); err != nil {
		return r, err
	}
	prefix := "/download/v" + version + "/"
	sums, _, err := u.request(ctx, prefix+"checksums.txt", 64<<10, false)
	if err != nil {
		return r, err
	}
	archives := map[string]string{}
	bodies := map[bool][]byte{}
	for _, gui := range []bool{false, true} {
		if gui && !s.GUI {
			continue
		}
		name := archiveName(version, s.Target, gui)
		hash, err := checksum(sums, name)
		if err != nil {
			if gui && strings.Contains(err.Error(), "missing_checksum") {
				return r, failure(app.CodeUpdateFailed, "gui_missing", err)
			}
			return r, err
		}
		body, _, err := u.request(ctx, prefix+name, archiveLimit, false, func(done, total int64) {
			if o.Progress != nil {
				o.Progress(Progress{Step: "download", Done: done, Total: total})
			}
		})
		if err != nil {
			return r, err
		}
		if err = verifyHash(body, hash); err != nil {
			return r, err
		}
		path := filepath.Join(dir, name)
		if err = writeExclusive(path, body, 0600); err != nil {
			return r, failure(app.CodeUpdateFailed, "swap_failed", err)
		}
		archives[name] = path
		bodies[gui] = body
		if o.Progress != nil {
			o.Progress(Progress{Step: "download", Done: int64(len(body)), Total: int64(len(body))})
		}
	}
	if err = step(ctx, o, "verify"); err != nil {
		return r, err
	}
	bundle, _, err := u.request(ctx, prefix+"checksums.txt.sigstore.json", 256<<10, false)
	var ae *app.Error
	if err != nil && (!errors.As(err, &ae) || ae.Code != app.CodeNotFound) {
		return r, err
	}
	signature, err := u.o.Verifier.Verify(ctx, VerifyInput{Tag: "v" + version, Checksums: sums, Bundle: bundle, Archives: archives})
	if err != nil {
		e := failure("integrity", "signature", err)
		e.Args["hint_key"] = "update.verification_hint"
		return r, e
	}
	r.signature = signature
	if err = step(ctx, o, "stage"); err != nil {
		return r, err
	}
	r.notices = map[string][]byte{}
	for _, gui := range []bool{false, true} {
		b, ok := bodies[gui]
		if !ok {
			continue
		}
		files, err := extract(b, dir, gui)
		if err != nil {
			return r, err
		}
		for name, data := range files {
			if strings.HasPrefix(name, "THIRD_PARTY/") || name == "LICENSE" {
				r.notices[name] = data
			}
		}
		r.files = append(r.files, binaryName(gui))
		if gui && runtime.GOOS == "windows" {
			r.files = append(r.files, "SDL2.dll")
		}
		if !gui {
			r.notes = releaseNotes(files["CHANGELOG.md"], u.o.Current)
		}
	}
	if err = u.probe(ctx, filepath.Join(dir, binaryName(false)), version, false, false); err != nil {
		if ctx.Err() != nil {
			return r, canceled(ctx)
		}
		return r, failure(app.CodeUpdateFailed, "check_failed", err)
	}
	if s.GUI {
		for _, eligibility := range []bool{false, true} {
			if err = u.probe(ctx, filepath.Join(dir, binaryName(true)), version, true, eligibility); err != nil {
				if ctx.Err() != nil {
					return r, canceled(ctx)
				}
				return r, failure(app.CodeUpdateFailed, "gui_not_eligible", err)
			}
		}
	}
	return r, nil
}
func releaseNotes(b []byte, current string) string {
	var lines []string
	include := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## [") {
			v, _, _ := strings.Cut(strings.TrimPrefix(line, "## ["), "]")
			include = validVersion(v) && compare(v, current) > 0
		}
		if !include {
			continue
		}
		if len(lines) >= 20 {
			break
		}
		lines = append(lines, app.SanitizeText(line))
	}
	notes := strings.Join(lines, "\n")
	if len(notes) > 2048 {
		notes = strings.ToValidUTF8(notes[:2048], "")
	}
	return notes
}
func (u *Updater) swap(ctx context.Context, o ApplyOptions, s State, stage string, r stagedRelease) error {
	dir := filepath.Dir(u.o.Exe)
	var swapped []string
	original := s
	aliasChanged := false
	rollback := func(reason string, cause error) error {
		restored := true
		for i := len(swapped) - 1; i >= 0; i-- {
			if err := restore(filepath.Join(dir, swapped[i])); err != nil {
				restored = false
				u.o.Log.Error("update rollback failed", "file", swapped[i])
			}
		}
		if aliasChanged && restored {
			id, e := relinkAlias(dir, original.Alias)
			if e != nil {
				restored = false
			} else {
				original.AliasFileID = id
				if e = atomicJSON(filepath.Join(dir, stateName), original, 0644); e != nil {
					restored = false
				}
			}
		}
		err := failure(app.CodeUpdateFailed, reason, cause)
		err.Args["restored"] = restored
		return err
	}
	for _, name := range r.files {
		path := filepath.Join(dir, name)
		if _, err := regular(path); err != nil {
			return rollback("swap_failed", err)
		}
		if err := clearBackup(path); err != nil {
			return rollback("swap_failed", err)
		}
		if err := backup(path); err != nil {
			return rollback("swap_failed", err)
		}
		swapped = append(swapped, name)
		if err := replaceFile(filepath.Join(stage, name), path); err != nil {
			return rollback("swap_failed", err)
		}
	}
	if runtime.GOOS == "windows" && s.Alias != "" {
		name := s.Alias + ".exe"
		path := filepath.Join(dir, name)
		cli := filepath.Join(dir, binaryName(false))
		old, err := regular(path)
		if err != nil {
			return rollback("swap_failed", err)
		}
		prior, err := regular(cli + ".old")
		recordedID, idErr := aliasFileID(path)
		if err != nil || (!os.SameFile(old, prior) && (s.AliasFileID == "" || idErr != nil || recordedID != s.AliasFileID)) {
			return rollback("swap_failed", fmt.Errorf("alias is no longer managed"))
		}
		if err = clearBackup(path); err != nil {
			return rollback("swap_failed", err)
		}
		if err = backup(path); err != nil {
			return rollback("swap_failed", err)
		}
		swapped = append(swapped, name)
		aliasChanged = true
		if err = os.Link(cli, path); err != nil {
			return rollback("swap_failed", err)
		}
		now, _ := os.Stat(path)
		main, _ := os.Stat(cli)
		if now == nil || main == nil || !os.SameFile(now, main) {
			return rollback("check_failed", fmt.Errorf("alias relink failed"))
		}
		s.AliasFileID, err = aliasFileID(path)
		if err != nil {
			return rollback("check_failed", err)
		}
	}
	if o.Progress != nil {
		o.Progress(Progress{Step: "check"})
	}
	if err := u.probe(ctx, filepath.Join(dir, binaryName(false)), r.version, false, false); err != nil {
		return rollback("check_failed", err)
	}
	if s.GUI {
		if err := u.probe(ctx, filepath.Join(dir, binaryName(true)), "", true, false); err != nil {
			return rollback("check_failed", err)
		}
	}
	// State is committed only after every binary passes. Preserve installer-owned fields.
	s.Version = r.version
	s.UpdatedAt = u.o.Now().UTC().Format("2006-01-02T15:04:05Z")
	s.UpdatedBy = "upgrade"
	if err := atomicJSON(filepath.Join(dir, stateName), s, 0644); err != nil {
		return rollback("swap_failed", err)
	}
	u.writeNotices(dir, r.notices)
	for _, name := range swapped {
		if err := os.Remove(filepath.Join(dir, name+".old")); err != nil {
			u.o.Log.Debug("update old file retained", "file", name)
		}
	}
	return nil
}
func clearBackup(path string) error {
	old := path + ".old"
	if _, err := regular(old); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(old); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	return retire(old, path)
}
func retire(src, base string) error {
	f, err := os.CreateTemp(filepath.Dir(base), filepath.Base(base)+".old-")
	if err != nil {
		return err
	}
	name := f.Name()
	if err = f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err = os.Remove(name); err != nil {
		return err
	}
	return os.Rename(src, name)
}

// relinkAlias is used only after the swap established ownership and rollback restored all binaries.
func relinkAlias(dir, alias string) (string, error) {
	path, cli := filepath.Join(dir, alias+".exe"), filepath.Join(dir, binaryName(false))
	target, err := regular(cli)
	if err != nil {
		return "", err
	}
	old, err := regular(path)
	if err == nil && os.SameFile(old, target) {
		return aliasFileID(path)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		if err = retire(path, path); err != nil {
			return "", err
		}
	}
	if err = os.Link(cli, path); err != nil {
		return "", err
	}
	linked, err := regular(path)
	if err != nil {
		return "", err
	}
	if !os.SameFile(linked, target) {
		return "", fmt.Errorf("alias relink mismatch")
	}
	return aliasFileID(path)
}

func (u *Updater) writeNotices(dir string, notices map[string][]byte) {
	for name, data := range notices {
		path := filepath.Join(dir, name)
		parent := filepath.Dir(path)
		if parent != dir {
			st, err := os.Lstat(parent)
			if os.IsNotExist(err) {
				err = os.Mkdir(parent, 0700)
				st, _ = os.Lstat(parent)
			}
			if err != nil || st == nil || !st.IsDir() || isReparse(st) || st.Mode()&os.ModeSymlink != 0 {
				u.o.Log.Warn("update licence write failed")
				continue
			}
		}
		if _, err := os.Lstat(path); err == nil {
			if _, err = regular(path); err != nil {
				u.o.Log.Warn("update licence write failed")
				continue
			}
		}
		f, err := os.CreateTemp(parent, ".mothdex-notice-")
		if err != nil {
			u.o.Log.Warn("update licence write failed")
			continue
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Chmod(0644)
		}
		if err == nil {
			err = f.Sync()
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		_ = os.Remove(f.Name())
		if err != nil {
			u.o.Log.Warn("update licence write failed")
		}
	}
}
