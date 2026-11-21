package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

func boxartRig(t *testing.T, mod func(*Env)) *dlRig {
	t.Helper()
	r := newDLRigEnv(t, DownloadConfig{}, mod)
	e := r.client.entries["game"]
	e.CoverURL = "https://img.test/game.png"
	r.client.entries["game"] = e
	r.client.entries["nocover"] = mothdex.Entry{Slug: "nocover", Title: "No Cover", Platform: "snes"}
	r.svc.o.Covers = &fakeCovers{store: r.svc.o.Cache, ok: true}
	return r
}

// The cover lands in the -o root as <title>.<ext>, written through the confined root; an existing
// file is kept, never replaced.
func TestSaveBoxartWritesOnceThroughTheConfinedRoot(t *testing.T) {
	r := boxartRig(t, nil)
	ctx := context.Background()
	path, err := r.svc.SaveBoxart(ctx, Ref{Slug: "game"}, r.out)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.out, "Game.png"); path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
	if b, _ := os.ReadFile(path); string(b) != "PNGDATA" { //nolint:gosec // reads a file the test wrote under t.TempDir
		t.Fatalf("content %q", b)
	}
	if err := os.WriteFile(path, []byte("MINE"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.SaveBoxart(ctx, Ref{Slug: "game"}, r.out)
	if err != nil || again != path {
		t.Fatalf("%q %v", again, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "MINE" { //nolint:gosec // reads a file the test wrote under t.TempDir
		t.Fatal("an existing cover was replaced")
	}
}

func TestSaveBoxartWithoutACoverIsNotFound(t *testing.T) {
	r := boxartRig(t, nil)
	if _, err := r.svc.SaveBoxart(context.Background(), Ref{Slug: "nocover"}, r.out); code(err) != CodeNotFound {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(r.out); err == nil {
		t.Fatal("a failed boxart must not create the output directory")
	}
}

// A launcher folder is refused for covers too (same Confiner as downloads).
func TestSaveBoxartRefusesLauncherFolders(t *testing.T) {
	romRoot := t.TempDir()
	prof := ProfileInfo{ID: "batocera-like", RomRoot: romRoot}
	prof.Profile.Roms.Root = romRoot
	prof.Profile.Launcher.Dir = filepath.Join(romRoot, "ports")
	if err := os.MkdirAll(prof.Profile.Launcher.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	r := boxartRig(t, func(e *Env) { e.Profile = prof })
	if _, err := r.svc.SaveBoxart(context.Background(), Ref{Slug: "game"}, filepath.Join(romRoot, "ports")); code(err) != "path_rejected" {
		t.Fatalf("a launcher folder must be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(romRoot, "ports", "Game.png")); err == nil {
		t.Fatal("a cover was written into a launcher folder")
	}
}

// Alicerce m1: after a download the cover is named after the file (the stem), in the file's folder,
// so a frontend pairs them; two links of one entry would not share one cover name.
func TestSaveJobBoxartIsNamedAfterTheDownloadedFile(t *testing.T) {
	r := boxartRig(t, nil)
	res := r.run(t, "game")
	if res.Done != 1 {
		t.Fatalf("%+v", res)
	}
	job := res.Jobs[0]
	path, err := r.svc.SaveJobBoxart(context.Background(), job, r.out)
	if err != nil {
		t.Fatal(err)
	}
	stem := strings.TrimSuffix(filepath.Base(job.Path), filepath.Ext(job.Path))
	if want := filepath.Join(filepath.Dir(job.Path), stem+".png"); path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
	if _, err := r.svc.SaveJobBoxart(context.Background(), JobSnapshot{Slug: "game", State: StateFailed}, r.out); code(err) != CodeInvalidRequest {
		t.Fatalf("a job that is not done has no cover to save: %v", err)
	}
}

// Crivo m1: a failed download carries the named values its message needs.
func TestFailedJobCarriesNamedValues(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	res := r.run(t, "bad")
	if res.Failed != 1 || res.Jobs[0].Err == nil {
		t.Fatalf("%+v", res)
	}
	a := res.Jobs[0].Err.Args
	for _, k := range []string{"path", "name", "host", "allowed", "got", "expected", "needed"} {
		if _, ok := a[k]; !ok {
			t.Errorf("missing named value %q: %v", k, a)
		}
	}
	if a["host"] != "proxy.mothdex.net" {
		t.Errorf("host %v", a["host"])
	}
}

// Crivo m3: export write failures map to stable filesystem codes.
func TestWriteExportMapsFailures(t *testing.T) {
	dir := t.TempDir()
	err := WriteExport(filepath.Join(dir, "missing", "x.csv"), false, func(io.Writer) error { return nil })
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != "path_rejected" || strings.Contains(ae.Message, ".mothdex-export") { // the Cause keeps the Go text for -v and the log
		t.Fatalf("%v", err)
	}
	target := filepath.Join(dir, "x.csv")
	if err := WriteExport(target, false, func(w io.Writer) error { _, e := w.Write([]byte("a")); return e }); err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(target, false, func(io.Writer) error { return nil }); code(err) != "exists" {
		t.Fatalf("%v", err)
	}
	if _, _, err := ExportTarget(filepath.Join(dir, "noext"), "", "search", time.Now()); code(err) != CodeUsage {
		t.Fatalf("%v", err)
	}
}
