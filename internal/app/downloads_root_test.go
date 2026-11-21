package app

import (
	"os"
	"path/filepath"
	"testing"
)

// T-20 criterion (T-17-audit): a relative -o becomes the absolute root, resolved once against the
// working directory at Enqueue time; the finished job reports an absolute path.
func TestRelativeOutputBecomesTheAbsoluteRoot(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	wd := t.TempDir()
	t.Chdir(wd)
	r.out = "rel-out"
	res := r.run(t, "game")
	if res.Done != 1 {
		t.Fatalf("%+v", res)
	}
	if got := res.Jobs[0].Path; !filepath.IsAbs(got) || got != filepath.Join(wd, "rel-out", "game.zip") {
		t.Fatalf("the job path is %q, want %q", got, filepath.Join(wd, "rel-out", "game.zip"))
	}
	if _, err := os.Stat(filepath.Join(wd, "rel-out", "game.zip")); err != nil {
		t.Fatal(err)
	}
}
