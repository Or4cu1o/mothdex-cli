package proxy_test

// T-16 catalog cases for destination, on_exists, disk and cleanup (13, 18 to 21, 23 to 25,
// 34 to 40). Each test names the case it covers.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/zipfix"
)

const mib = int64(1) << 20

// plant creates a file inside the fake FS (parents included) and returns its real path.
func plant(t *testing.T, r *rig, name, data string) string {
	t.Helper()
	p := r.fs.Path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func stagingEmpty(t *testing.T, r *rig) {
	t.Helper()
	if entries, _ := os.ReadDir(r.fs.Path("downloads")); len(entries) != 0 {
		t.Fatalf("staging not empty: %v", entries)
	}
}

// Case 13: a platform id that is not exactly one of the known ones fails before any file exists.
func TestCase13PlatformIDInjection(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.srv.Script(fakeproxy.AnyPath, fakeproxy.OK(fakeproxy.Pattern(10)))
	before := realfs.Snapshot(t, r.fs.Dir())
	for _, id := range []string{"../../system", "ps1/../..", "PS1", "ps1 ", "", "zzz", "ps1\x00", "ps1/"} {
		job := r.job("/f/a", "a.zip", 10)
		job.Platform = id
		wantFailed(t, r.download(job), proxy.CodePathRejected)
	}
	if d := realfs.DiffSnapshots(before, realfs.Snapshot(t, r.fs.Dir())); len(d) != 0 {
		t.Fatalf("files were created: %v", d)
	}
	if r.srv.Connections() != 0 {
		t.Fatal("a rejected platform must not reach the network")
	}
}

// Case 18: a symlink already sitting at the final name is never followed, whatever on_exists says.
func TestCase18SymlinkAtTheFinalName(t *testing.T) {
	policies := []struct {
		name  string
		cfg   proxy.Config
		state proxy.State
		final string
	}{
		{"skip", proxy.Config{OnExists: proxy.OnExistsSkip}, proxy.StateSkipped, ""},
		{"rename", proxy.Config{OnExists: proxy.OnExistsRename}, proxy.StateDone, "roms/snes/x (1).zip"},
		{"overwrite", proxy.Config{OnExists: proxy.OnExistsOverwrite, Force: true}, proxy.StateFailed, ""},
	}
	for _, p := range policies {
		t.Run(p.name, func(t *testing.T) {
			r := newRig(t, p.cfg)
			victim := filepath.Join(realfs.Dir(t), "victim")
			realfs.WriteFile(t, victim, []byte("precious"))
			if err := os.MkdirAll(r.fs.Path("roms/snes"), 0o700); err != nil {
				t.Fatal(err)
			}
			realfs.Symlink(t, victim, r.fs.Path("roms/snes/x.zip"))
			body := fakeproxy.Pattern(500)
			r.srv.Script("/f/x", fakeproxy.OK(body))

			res := r.download(r.job("/f/x", "x.zip", len(body)))

			if res.State != p.state {
				t.Fatalf("state %s err %v, want %s", res.State, res.Err, p.state)
			}
			if p.state == proxy.StateFailed && res.Err.Code != proxy.CodePathRejected {
				t.Fatalf("code %s, want path_rejected (target is not a regular file)", res.Err.Code)
			}
			if got, _ := os.ReadFile(victim); string(got) != "precious" { //nolint:gosec // test path
				t.Fatalf("the symlink target was written: %q", got)
			}
			if p.final != "" {
				if got, _ := os.ReadFile(r.fs.Path(p.final)); !bytes.Equal(got, body) { //nolint:gosec // test path
					t.Fatalf("renamed file missing or wrong")
				}
			}
		})
	}
}

// Case 19: a file or symlink planted at the name of the cross-device temporary copy makes the
// job fail; nothing outside the destination changes.
func TestCase19PreplantedTemporaryName(t *testing.T) {
	for _, kind := range []string{"symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			staging := fakefs.New(t) // a second file system on another device: the copy path, .<jobID>.mothdex-tmp
			staging.SetVolume(".", 1<<50, 2)
			r.rebuild(proxy.Config{}, func(o *proxy.Options) {
				o.Staging = staging
				o.IDs = bytes.NewReader(make([]byte, 16)) // job id "aaaaaaaaaaaaaaaaaaaaaaaaaa"
			})
			victim := filepath.Join(realfs.Dir(t), "victim")
			realfs.WriteFile(t, victim, []byte("precious"))
			tmp := r.fs.Path("roms/snes/." + strings.Repeat("a", 26) + ".mothdex-tmp")
			if err := os.MkdirAll(filepath.Dir(tmp), 0o700); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				realfs.Symlink(t, victim, tmp)
			} else if err := os.WriteFile(tmp, []byte("squatter"), 0o600); err != nil {
				t.Fatal(err)
			}
			body := fakeproxy.Pattern(500)
			r.srv.Script("/f/x", fakeproxy.OK(body))

			res := r.download(r.job("/f/x", "x.zip", len(body)))

			if res.State == proxy.StateDone {
				t.Fatal("a job must not succeed over a squatted temporary name")
			}
			if got, _ := os.ReadFile(victim); string(got) != "precious" { //nolint:gosec // test path
				t.Fatalf("the symlink target was written: %q", got)
			}
			if _, err := os.Lstat(r.fs.Path("roms/snes/x.zip")); err == nil {
				t.Fatal("a final file exists")
			}
		})
	}
}

// Case 20: skip leaves the file alone, makes no request and leaves no staging file.
func TestCase20SkipLeavesNoStagingFile(t *testing.T) {
	r := newRig(t, proxy.Config{})
	plant(t, r, "roms/snes/x.zip", "mine")
	r.srv.Script("/f/x", fakeproxy.OK(fakeproxy.Pattern(10)))
	if res := r.download(r.job("/f/x", "x.zip", 10)); res.State != proxy.StateSkipped {
		t.Fatalf("state %s", res.State)
	}
	if got, _ := os.ReadFile(r.fs.Path("roms/snes/x.zip")); string(got) != "mine" {
		t.Fatal("the existing file changed")
	}
	stagingEmpty(t, r)
	if len(r.history.recs) != 1 || r.history.recs[0].State != proxy.StateSkipped {
		t.Fatalf("history %+v", r.history.recs)
	}
}

// Case 21: a second writer creates the final name between Preflight and the rename.
func TestCase21RaceAtFinalize(t *testing.T) {
	cases := []struct {
		name  string
		cfg   proxy.Config
		state proxy.State
		check func(t *testing.T, r *rig, body []byte)
	}{
		{"skip", proxy.Config{OnExists: proxy.OnExistsSkip}, proxy.StateSkipped, func(t *testing.T, r *rig, _ []byte) {
			t.Helper()
			if got, _ := os.ReadFile(r.fs.Path("roms/snes/x.zip")); string(got) != "second writer" {
				t.Fatalf("the second writer's file was replaced: %q", got)
			}
		}},
		{"rename", proxy.Config{OnExists: proxy.OnExistsRename}, proxy.StateDone, func(t *testing.T, r *rig, body []byte) {
			t.Helper()
			if got, _ := os.ReadFile(r.fs.Path("roms/snes/x.zip")); string(got) != "second writer" {
				t.Fatalf("the second writer's file was replaced: %q", got)
			}
			if got, _ := os.ReadFile(r.fs.Path("roms/snes/x (1).zip")); !bytes.Equal(got, body) {
				t.Fatal("our file must land under the next free name")
			}
		}},
		{"overwrite", proxy.Config{OnExists: proxy.OnExistsOverwrite, Force: true}, proxy.StateDone, func(t *testing.T, r *rig, body []byte) {
			t.Helper()
			if got, _ := os.ReadFile(r.fs.Path("roms/snes/x.zip")); !bytes.Equal(got, body) {
				t.Fatal("--force must replace the file")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.cfg)
			body := fakeproxy.Pattern(500)
			r.srv.Script("/f/x", fakeproxy.OK(body))
			r.fs.Before(fakefs.OpRename, "roms/snes/x.zip", func() { plant(t, r, "roms/snes/x.zip", "second writer") })

			res := r.download(r.job("/f/x", "x.zip", len(body)))

			if res.State != c.state {
				t.Fatalf("state %s err %v, want %s", res.State, res.Err, c.state)
			}
			c.check(t, r, body)
			stagingEmpty(t, r)
		})
	}
}

// Case 23: rename gives up after (99): no hundredth file is created.
func TestCase23RenameExhaustsAt99(t *testing.T) {
	r := newRig(t, proxy.Config{OnExists: proxy.OnExistsRename})
	plant(t, r, "roms/snes/name.zip", "0")
	for n := 1; n <= 99; n++ {
		plant(t, r, fmt.Sprintf("roms/snes/name (%d).zip", n), "x")
	}
	r.srv.Script("/f/n", fakeproxy.OK(fakeproxy.Pattern(10)))
	wantFailed(t, r.download(r.job("/f/n", "name.zip", 10)), proxy.CodeExists)
	if names := realfs.Names(t, r.fs.Path("roms/snes")); len(names) != 100 || slices.Contains(names, "name (100).zip") {
		t.Fatalf("%d entries, a 100th name was created", len(names))
	}
	stagingEmpty(t, r)
}

// Case 24: two jobs of one batch that sanitize to the same name never overwrite each other.
func TestCase24BatchNameCollisions(t *testing.T) {
	pairs := map[string][2]string{"colon": {"A:B.zip", "A_B.zip"}, "case": {"Game.zip", "game.zip"}, "same": {"x.zip", "x.zip"}}
	for name, names := range pairs {
		for _, c := range []struct {
			policy      proxy.OnExists
			concurrency int
		}{{proxy.OnExistsSkip, 1}, {proxy.OnExistsSkip, 2}, {proxy.OnExistsRename, 1}, {proxy.OnExistsRename, 2}} {
			policy := c.policy
			t.Run(fmt.Sprintf("%s/%s/%d", name, policy, c.concurrency), func(t *testing.T) {
				// concurrency 1 runs the jobs one after the other: the first has finished and
				// released its name before the second starts, which is the case a plain
				// "name is held" check misses.
				r := newRig(t, proxy.Config{OnExists: policy, Concurrency: c.concurrency})
				bodies := [2][]byte{bytes.Repeat([]byte("a"), 400), bytes.Repeat([]byte("b"), 400)}
				r.srv.Script("/f/a", fakeproxy.OK(bodies[0]))
				r.srv.Script("/f/b", fakeproxy.OK(bodies[1]))
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				results := r.dl.Run(ctx, []proxy.Job{r.job("/f/a", names[0], 400), r.job("/f/b", names[1], 400)})
				done := 0
				for i, res := range results {
					switch res.State {
					case proxy.StateDone:
						done++
						if got, _ := os.ReadFile(res.Path); !bytes.Equal(got, bodies[i]) { //nolint:gosec // test path
							t.Fatalf("job %d: its file was overwritten (%s)", i, res.Path)
						}
					case proxy.StateSkipped:
					default:
						t.Fatalf("job %d: state %s err %v", i, res.State, res.Err)
					}
				}
				if policy == proxy.OnExistsRename && done != 2 {
					t.Fatalf("rename: %d jobs done, want 2", done)
				}
				if policy == proxy.OnExistsSkip && done < 1 {
					t.Fatalf("skip: %d jobs done, at least one must write its file", done)
				}
				if policy == proxy.OnExistsSkip && done != 1 { // "Game.zip" and "game.zip" collide on every file system
					t.Fatalf("skip: %d jobs done, want exactly one (the names collide)", done)
				}
				if len(r.history.recs) != 2 {
					t.Fatalf("history has %d records, want both outcomes", len(r.history.recs))
				}
				// Neither name may be lost to the case-folding or the sanitizing: one file per Done job.
				if files := realfs.Names(t, r.fs.Path("roms/snes")); len(files) != done {
					t.Fatalf("%d jobs done but the directory holds %v", done, files)
				}
			})
		}
	}
}

// Case 25: on a case-insensitive file system an existing GAME.ZIP is an existing game.zip.
func TestCase25CaseInsensitiveFileSystem(t *testing.T) {
	r := newRig(t, proxy.Config{})
	if !realfs.IsCaseInsensitive(t, r.fs.Dir()) {
		t.Skip("case-sensitive file system")
	}
	plant(t, r, "roms/snes/GAME.ZIP", "mine")
	r.srv.Script("/f/g", fakeproxy.OK(fakeproxy.Pattern(10)))
	if res := r.download(r.job("/f/g", "game.zip", 10)); res.State != proxy.StateSkipped {
		t.Fatalf("state %s", res.State)
	}
}

// diskFullErr is what a full disk reports on this OS; the classifier is OS-guarded.
func diskFullErr() error {
	if runtime.GOOS == "windows" {
		return fakefs.ErrDiskFullWindows
	}
	return fakefs.ErrNoSpace
}

// Case 34: ENOSPC mid-stream fails the job as disk_full without a retry and frees everything.
func TestCase34DiskFullMidStream(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.fs.SetVolume(".", 300*mib, 1) // room for exactly one reservation
	body := fakeproxy.Pattern(100_000)
	r.srv.Script("/f/a", fakeproxy.OK(body))
	r.fs.LimitWrites(10_000, diskFullErr())

	res := r.download(r.job("/f/a", "a.zip", len(body)))

	wantFailed(t, res, proxy.CodeDiskFull)
	if res.Attempts != 1 || len(r.srv.Requests()) != 1 {
		t.Fatalf("attempts %d, requests %d: a full disk is not retried", res.Attempts, len(r.srv.Requests()))
	}
	stagingEmpty(t, r)
	noFinalFile(t, r)

	r.fs.LimitWrites(1<<40, diskFullErr()) // the disk has room again
	r.srv.Script("/f/b", fakeproxy.OK(body))
	if res := r.download(r.job("/f/b", "b.zip", len(body))); res.State != proxy.StateDone {
		t.Fatalf("the reservation of the failed job was not released: %s %v", res.State, res.Err)
	}
}

// Case 35: reservations add up across concurrent jobs.
func TestCase35ReservationAcrossJobs(t *testing.T) {
	r := newRig(t, proxy.Config{Concurrency: 2})
	r.fs.SetVolume(".", 400*mib, 1) // one job needs about 256 MiB: two do not fit
	body := fakeproxy.Pattern(1000)
	r.srv.Script("/f/a", fakeproxy.StallBeforeHeaders(body, time.Hour))
	r.srv.Script("/f/b", fakeproxy.OK(body))

	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan proxy.Result, 1)
	go func() { first <- r.dl.Download(ctx, r.job("/f/a", "a.zip", len(body))) }()
	r.clock.BlockUntil(t, 1) // the first job is in flight and holds its reservation

	wantFailed(t, r.download(r.job("/f/b", "b.zip", len(body))), proxy.CodeDiskFull)
	for _, rq := range r.srv.Requests() {
		if rq.Path == "/f/b" {
			t.Fatal("the second job must fail in Preflight, before connecting")
		}
	}
	cancel()
	if res := <-first; res.State != proxy.StateCanceled {
		t.Fatalf("first job: %s", res.State)
	}
}

// Case 36: enough space in staging but not in the destination volume fails in Preflight.
func TestCase36TwoFileSystems(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.fs.SetVolume("downloads", 100<<30, 1)
	r.fs.SetVolume("roms", 10*mib, 2)
	r.srv.Script("/f/a", fakeproxy.OK(fakeproxy.Pattern(1000)))
	wantFailed(t, r.download(r.job("/f/a", "a.zip", 1000)), proxy.CodeDiskFull)
	if r.srv.Connections() != 0 {
		t.Fatal("Preflight must fail before any connection")
	}
}

// Case 37: a cross-device rename falls back to a flushed copy inside the destination.
func TestCase37CrossDeviceCopyPath(t *testing.T) {
	t.Run("copy then rename", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		body := fakeproxy.Pattern(2000)
		r.srv.Script("/f/a", fakeproxy.OK(body))
		r.fs.Fail(fakefs.OpRename, "*", fakefs.ErrCrossDevice, 1)

		res := r.download(r.job("/f/a", "a.zip", len(body)))

		if res.State != proxy.StateDone {
			t.Fatalf("state %s err %v", res.State, res.Err)
		}
		if got, _ := os.ReadFile(r.fs.Path("roms/snes/a.zip")); !bytes.Equal(got, body) {
			t.Fatal("final content differs")
		}
		var tmp string
		for _, c := range r.fs.Calls() {
			if c.Op == fakefs.OpOpenExcl && strings.HasSuffix(c.Name, ".mothdex-tmp") {
				tmp = c.Name
			}
		}
		if tmp == "" || !strings.HasPrefix(tmp, "roms/snes/.") {
			t.Fatalf("temporary copy %q is not inside the destination directory", tmp)
		}
		if r.fs.Syncs(tmp) == 0 {
			t.Fatal("the copy must be flushed before the final rename")
		}
		stagingEmpty(t, r)
		if _, err := os.Lstat(r.fs.Path(tmp)); err == nil {
			t.Fatal("the temporary copy was left behind")
		}
	})
	t.Run("a failing rename never leaves a partial final name", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		body := fakeproxy.Pattern(2000)
		r.srv.Script("/f/a", fakeproxy.OK(body))
		r.fs.Fail(fakefs.OpRename, "*", fakefs.ErrCrossDevice, 0)

		res := r.download(r.job("/f/a", "a.zip", len(body)))

		if res.State == proxy.StateDone {
			t.Fatal("the job cannot succeed when no rename works")
		}
		noFinalFile(t, r)
		for name, kind := range realfs.Snapshot(t, r.fs.Dir()) {
			if strings.Contains(name, "mothdex-tmp") && kind != "dir" {
				t.Fatalf("temporary copy left behind: %s", name)
			}
		}
	})
}

// cancelOn returns a rig whose context is canceled when the job reaches state.
func cancelOn(t *testing.T, cfg proxy.Config, state proxy.State) (*rig, context.Context) {
	t.Helper()
	r := newRig(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	r.rebuild(cfg, func(o *proxy.Options) {
		inner := o.OnEvent
		o.OnEvent = func(e proxy.Event) {
			inner(e)
			if e.State == state {
				cancel()
			}
		}
	})
	return r, ctx
}

// Case 38: a cancel while Transferring or Verifying removes staging and writes one record.
func TestCase38CancelInTransferringAndVerifying(t *testing.T) {
	for _, state := range []proxy.State{proxy.StateTransferring, proxy.StateVerifying} {
		t.Run(string(state), func(t *testing.T) {
			r, ctx := cancelOn(t, proxy.Config{}, state)
			body := zipfix.Valid(map[string][]byte{"a.bin": fakeproxy.Pattern(20_000), "b.bin": fakeproxy.Pattern(20_000)})
			r.srv.Script("/f/a", fakeproxy.StallMidStream(body, len(body)/2, time.Hour))
			if state == proxy.StateVerifying {
				r.srv.Script("/f/a", fakeproxy.OK(body))
			}

			res := r.dl.Download(ctx, r.job("/f/a", "a.zip", len(body)))

			if res.State != proxy.StateCanceled || res.Err == nil || res.Err.Code != proxy.CodeCanceled {
				t.Fatalf("state %s err %v", res.State, res.Err)
			}
			stagingEmpty(t, r)
			noFinalFile(t, r)
			if len(r.history.recs) != 1 || r.history.recs[0].State != proxy.StateCanceled {
				t.Fatalf("history %+v", r.history.recs)
			}
			if left, _ := r.journal.Entries(); len(left) != 0 {
				t.Fatalf("journal not cleared: %v", left)
			}
		})
	}
}

// Case 39: startup cleanup removes only old, regular, exactly named leftovers.
func TestCase39StartupCleanup(t *testing.T) {
	r := newRig(t, proxy.Config{})
	id := func(c string) string { return strings.Repeat(c, 26) }
	age := func(p string, h int) {
		t.Helper()
		when := r.clock.Now().Add(-time.Duration(h) * time.Hour)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(realfs.Dir(t), "victim")
	realfs.WriteFile(t, victim, []byte("precious"))
	age(victim, 48) // a cleanup that follows the link would see an old regular file and delete it
	old := plant(t, r, "downloads/"+id("a")+".part", "x")
	young := plant(t, r, "downloads/"+id("b")+".part", "x")
	notes := plant(t, r, "downloads/notes.part", "x")
	age(old, 25)
	age(young, 23)
	age(notes, 48)
	link := r.fs.Path("downloads/" + id("c") + ".part")
	realfs.Symlink(t, victim, link)
	ageLink(t, link, r.clock.Now().Add(-48*time.Hour))
	dir := r.fs.Path("downloads/" + id("d") + ".part")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	age(dir, 48)
	oldTmp := plant(t, r, "roms/snes/."+id("e")+".mothdex-tmp", "x")
	youngTmp := plant(t, r, "roms/snes/."+id("f")+".mothdex-tmp", "x")
	age(oldTmp, 30)
	age(youngTmp, 1)
	for _, c := range []string{"e", "f"} {
		if err := r.journal.Add(proxy.JournalEntry{JobID: id(c), Staging: "downloads/" + id(c) + ".part", DestDir: "roms/snes", Started: r.clock.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.dl.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}

	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	for p, want := range map[string]bool{old: false, young: true, notes: true, link: true, dir: true, oldTmp: false, youngTmp: true} {
		if exists(p) != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(p), exists(p), want)
		}
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "precious" { //nolint:gosec // test path
		t.Fatalf("a symlink was followed and its target removed or changed: %q %v", got, err)
	}
	left, _ := r.journal.Entries()
	if len(left) != 1 || left[0].JobID != id("f") {
		t.Fatalf("journal after cleanup: %+v", left)
	}
}

// Case 40: a retry writes through the descriptor it already holds, never through the path, and
// finalize notices that the staging name no longer is that file (Crivo addendum): the job does
// not report Done over a planted link.
func TestCase40RetryNeverWritesThroughASwappedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an open file cannot be unlinked on Windows, so the path swap cannot happen")
	}
	for _, crossDevice := range []bool{false, true} {
		t.Run(map[bool]string{false: "staged in the destination", true: "staged in paths.temp"}[crossDevice], func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			if crossDevice {
				r.fs.SetVolume("downloads", 1<<50, 2)
			}
			victim := filepath.Join(realfs.Dir(t), "victim")
			realfs.WriteFile(t, victim, []byte("precious"))
			swapped := false
			r.rebuild(proxy.Config{}, func(o *proxy.Options) {
				inner := o.OnEvent
				o.OnEvent = func(e proxy.Event) {
					inner(e)
					if e.State != proxy.StateRetryWait || swapped {
						return
					}
					swapped = true
					p := r.fs.Path("roms/snes/." + e.JobID + ".part")
					if crossDevice {
						p = r.fs.Path("downloads/" + e.JobID + ".part")
					}
					if err := os.Remove(p); err != nil {
						t.Error(err)
					}
					realfs.Symlink(t, victim, p)
				}
			})
			body := fakeproxy.Pattern(50_000)
			r.srv.Script("/f/a", fakeproxy.Truncate(body, 10_000), fakeproxy.OK(body))

			res := r.download(r.job("/f/a", "a.zip", len(body)))

			if !swapped {
				t.Fatal("the retry state was never reached")
			}
			if got, _ := os.ReadFile(victim); string(got) != "precious" { //nolint:gosec // test path
				t.Fatalf("the symlink target was written: %d bytes", len(got))
			}
			final := r.fs.Path("roms/snes/a.zip")
			info, err := os.Lstat(final)
			switch {
			case res.State == proxy.StateDone:
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("Done but the final entry is not a regular file: %v %v", info, err)
				}
				if got, _ := os.ReadFile(final); !bytes.Equal(got, body) { //nolint:gosec // test path
					t.Fatal("Done with the wrong content")
				}
			case err == nil:
				t.Fatalf("state %s but a final entry exists (%v)", res.State, info.Mode())
			}
		})
	}
}

// The finalize fallback chain (downloader.md 6.3): no-replace rename, then a hard link, then an
// O_EXCL placeholder replaced by the final rename. Each step must place the file and must never
// replace a file that a second writer created in the meantime.
func TestFinalizeFallbackChain(t *testing.T) {
	body := fakeproxy.Pattern(2000)
	setup := func(t *testing.T, cfg proxy.Config, linkUnsupported bool) *rig {
		t.Helper()
		r := newRig(t, cfg)
		r.srv.Script("/f/a", fakeproxy.OK(body))
		r.fs.Fail(fakefs.OpRename, "*", fakefs.ErrNoReplaceUnsupported, 1)
		if linkUnsupported {
			r.fs.Fail(fakefs.OpLink, "*", fakefs.ErrHardLinkUnsupported, 1)
		}
		return r
	}
	called := func(r *rig, op, name string) bool {
		for _, c := range r.fs.Calls() {
			if c.Op == op && (name == "" || c.Name == name || c.To == name) {
				return true
			}
		}
		return false
	}
	const final = "roms/snes/a.zip"

	t.Run("hard link when no-replace rename is unsupported", func(t *testing.T) {
		r := setup(t, proxy.Config{}, false)
		if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateDone {
			t.Fatalf("state %s err %v", res.State, res.Err)
		}
		if !called(r, fakefs.OpLink, final) {
			t.Fatal("the hard link step was not used")
		}
		if got, _ := os.ReadFile(r.fs.Path(final)); !bytes.Equal(got, body) {
			t.Fatal("final content differs")
		}
		stagingEmpty(t, r)
	})
	t.Run("placeholder when links are unsupported too", func(t *testing.T) {
		r := setup(t, proxy.Config{}, true)
		if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateDone {
			t.Fatalf("state %s err %v", res.State, res.Err)
		}
		if !called(r, fakefs.OpOpenExcl, final) {
			t.Fatal("the O_EXCL placeholder step was not used")
		}
		if got, _ := os.ReadFile(r.fs.Path(final)); !bytes.Equal(got, body) {
			t.Fatal("final content differs")
		}
		stagingEmpty(t, r)
	})
	t.Run("a second writer wins against the hard link step", func(t *testing.T) {
		r := setup(t, proxy.Config{}, false)
		r.fs.Before(fakefs.OpLink, final, func() { plant(t, r, final, "second writer") })
		if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateSkipped {
			t.Fatalf("state %s err %v, want skipped", res.State, res.Err)
		}
		if got, _ := os.ReadFile(r.fs.Path(final)); string(got) != "second writer" {
			t.Fatalf("the second writer's file was replaced: %q", got)
		}
		stagingEmpty(t, r)
	})
	t.Run("a second writer wins against the placeholder step", func(t *testing.T) {
		r := setup(t, proxy.Config{}, true)
		r.fs.Before(fakefs.OpOpenExcl, final, func() { plant(t, r, final, "second writer") })
		if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateSkipped {
			t.Fatalf("state %s err %v, want skipped", res.State, res.Err)
		}
		if got, _ := os.ReadFile(r.fs.Path(final)); string(got) != "second writer" {
			t.Fatalf("the second writer's file was replaced: %q", got)
		}
		stagingEmpty(t, r)
	})
	t.Run("rename policy moves on to the next name in every step", func(t *testing.T) {
		r := setup(t, proxy.Config{OnExists: proxy.OnExistsRename}, true)
		r.fs.Before(fakefs.OpOpenExcl, final, func() { plant(t, r, final, "second writer") })
		if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateDone {
			t.Fatalf("state %s err %v", res.State, res.Err)
		}
		if got, _ := os.ReadFile(r.fs.Path(final)); string(got) != "second writer" {
			t.Fatalf("the second writer's file was replaced: %q", got)
		}
		if got, _ := os.ReadFile(r.fs.Path("roms/snes/a (1).zip")); !bytes.Equal(got, body) {
			t.Fatal("our file must land under the next free name")
		}
	})
}
