package proxy_test

// T-16 catalog cases that need the real, confined file system: symlinked and junctioned
// destination directories (16, 17) and launcher folders (15, downloader side). These run the
// production RootFS, Confiner and per-OS Finalizer end to end, with no double.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
)

// realLayout wires a rig to the production file system code: staging and the ROM root are two
// real directories, so the copy path and the OS no-replace rename are both exercised.
func realLayout(t *testing.T, cfg proxy.Config, conf func(*proxy.Confiner)) (r *rig, root string) {
	t.Helper()
	r = newRig(t, cfg)
	root = filepath.Join(realfs.Dir(t), "roms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	staging, err := proxy.OpenRootFS(realfs.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	dest, err := proxy.OpenRootFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staging.Close(); _ = dest.Close() })
	c := &proxy.Confiner{FS: dest}
	if conf != nil {
		conf(c)
	}
	r.rebuild(cfg, func(o *proxy.Options) {
		o.Staging, o.Dest, o.Finalizer, o.Confine = staging, dest, proxy.NewFinalizer(), c.Resolve
	})
	return r, root
}

func job(r *rig, destDir string, body []byte) proxy.Job {
	j := r.job("/f/a", "a.zip", len(body))
	j.DestDir = destDir
	return j
}

// linkedDestination is shared by cases 16 and 17: the link inside the root leads outside it.
func linkedDestination(t *testing.T, link func(testing.TB, string, string)) {
	t.Helper()
	r, root := realLayout(t, proxy.Config{}, nil)
	body := fakeproxy.Pattern(500)
	r.srv.Script("/f/a", fakeproxy.OK(body))
	outside := filepath.Join(realfs.Dir(t), "elsewhere")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link(t, outside, filepath.Join(root, "snes"))
	before := realfs.Snapshot(t, outside)

	for _, dest := range []string{filepath.Join(root, "snes"), filepath.Join(root, "snes", "sub")} {
		wantFailed(t, r.download(job(r, dest, body)), proxy.CodePathRejected)
	}
	if d := realfs.DiffSnapshots(before, realfs.Snapshot(t, outside)); len(d) != 0 {
		t.Fatalf("something was written outside the root: %v", d)
	}
	if r.srv.Connections() != 0 {
		t.Fatal("a rejected destination must not cost a download")
	}

	// -o with the real path makes that directory the root: it works.
	staging, _ := proxy.OpenRootFS(realfs.Dir(t))
	dest, err := proxy.OpenRootFS(outside)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staging.Close(); _ = dest.Close() })
	c := &proxy.Confiner{FS: dest}
	r.rebuild(proxy.Config{}, func(o *proxy.Options) {
		o.Staging, o.Dest, o.Finalizer, o.Confine = staging, dest, proxy.NewFinalizer(), c.Resolve
	})
	if res := r.download(job(r, outside, body)); res.State != proxy.StateDone {
		t.Fatalf("-o with the real path: %s %v", res.State, res.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "a.zip")); string(got) != string(body) { //nolint:gosec // test path
		t.Fatal("the file is not at the real path")
	}
}

// Case 16: a symlinked destination directory is refused; -o with the real path succeeds.
func TestCase16SymlinkedDestination(t *testing.T) { linkedDestination(t, realfs.Symlink) }

// Case 17: the same with a directory junction (a symlink on Unix; a real junction on Windows).
func TestCase17JunctionDestination(t *testing.T) { linkedDestination(t, realfs.DirLink) }

// Case 15 (downloader side): a launcher folder is never a destination, however it is reached.
func TestCase15LauncherFolderAsDestination(t *testing.T) {
	isLauncher := func(abs string) bool {
		for _, part := range strings.Split(filepath.ToSlash(abs), "/") {
			if part == "ports" || part == "ports_scripts" || part == "windows" {
				return true
			}
		}
		return false
	}
	r, root := realLayout(t, proxy.Config{}, func(c *proxy.Confiner) { c.IsLauncher = isLauncher })
	body := fakeproxy.Pattern(500)
	r.srv.Script("/f/a", fakeproxy.OK(body))
	for _, dir := range []string{"ports", "ports_scripts", "windows", "ports/sub"} {
		wantFailed(t, r.download(job(r, filepath.Join(root, dir), body)), proxy.CodePathRejected)
	}
	if r.srv.Connections() != 0 {
		t.Fatal("a launcher destination must be refused before any download")
	}
	if res := r.download(job(r, filepath.Join(root, "snes"), body)); res.State != proxy.StateDone {
		t.Fatalf("a normal folder: %s %v", res.State, res.Err)
	}
}
