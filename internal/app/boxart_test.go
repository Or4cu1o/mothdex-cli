package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
