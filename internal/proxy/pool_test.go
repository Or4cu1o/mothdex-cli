package proxy_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
)

// Batch overwrite (Alicerce M1): three jobs with the same name under overwrite and --force,
// one at a time, must give name, name (1), name (2): a finished file of the batch is never
// replaced by a later job.
func TestOverwriteNeverReplacesAFileOfTheSameBatch(t *testing.T) {
	r := newRig(t, proxy.Config{OnExists: proxy.OnExistsOverwrite, Force: true, Concurrency: 1})
	jobs := make([]proxy.Job, 3)
	for i := range jobs {
		body := fakeproxy.Pattern(100 + i)
		path := fmt.Sprintf("/f/%d", i)
		r.srv.Script(path, fakeproxy.OK(body))
		jobs[i] = r.job(path, "same.zip", len(body))
	}

	for i, res := range r.dl.Run(context.Background(), jobs) {
		if res.State != proxy.StateDone {
			t.Fatalf("job %d: %s %v", i, res.State, res.Err)
		}
	}
	for i, name := range []string{"same.zip", "same (1).zip", "same (2).zip"} {
		info, err := os.Stat(r.fs.Path("roms/snes/" + name))
		if err != nil || info.Size() != int64(100+i) {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
}

// Single write (Alicerce M2): staging and destination on one device means the file is staged
// inside the destination and renamed, never copied.
func TestSameDeviceIsStagedInTheDestinationAndNeverCopied(t *testing.T) {
	r := newRig(t, proxy.Config{})
	body := fakeproxy.Pattern(2000)
	r.srv.Script("/f/a", fakeproxy.OK(body))

	if res := r.download(r.job("/f/a", "a.zip", len(body))); res.State != proxy.StateDone {
		t.Fatalf("%s %v", res.State, res.Err)
	}
	staged := false
	for _, c := range r.fs.Calls() {
		if c.Op == "OpenExcl" && strings.HasSuffix(c.Name, ".mothdex-tmp") {
			t.Fatal("a same-device download must not make a cross-device copy")
		}
		if c.Op == "OpenExcl" && strings.HasPrefix(c.Name, "roms/snes/.") && strings.HasSuffix(c.Name, ".part") {
			staged = true
		}
	}
	if !staged {
		t.Fatal("the staging file must live in the destination directory")
	}
}

// Order and sharing (Crivo minors 1 and 2): one worker runs jobs in submission order, Queued is
// emitted for every job when it is accepted, and Cancel stops one job only.
func TestPoolIsFIFOQueuedAtSubmitAndCancelsOneJob(t *testing.T) {
	r := newRig(t, proxy.Config{Concurrency: 1})
	r.srv.Script("/f/slow", fakeproxy.StallBeforeFirstByte(fakeproxy.Pattern(50), time.Hour))
	var ids []string
	var chans []<-chan proxy.Result
	for i := range 6 {
		path := fmt.Sprintf("/f/%d", i)
		body := fakeproxy.Pattern(50 + i)
		r.srv.Script(path, fakeproxy.OK(body))
		job := r.job(path, fmt.Sprintf("g%d.zip", i), len(body))
		if i == 0 {
			job = r.job("/f/slow", "slow.zip", 50)
		}
		id, ch, err := r.dl.Submit(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}
		ids, chans = append(ids, id), append(chans, ch)
	}
	if queued := r.countEvents(proxy.StateQueued); queued != 6 {
		t.Fatalf("Queued must be emitted at submission for every job, got %d", queued)
	}
	if !r.dl.Cancel(ids[0]) || r.dl.Cancel("nope") {
		t.Fatal("Cancel must know the id of a job and refuse an unknown one")
	}
	if res := <-chans[0]; res.State != proxy.StateCanceled {
		t.Fatalf("the canceled job: %s %v", res.State, res.Err)
	}
	for i := 1; i < 6; i++ {
		if res := <-chans[i]; res.State != proxy.StateDone {
			t.Fatalf("job %d: %s %v", i, res.State, res.Err)
		}
	}
	var order []string
	r.evMu.Lock()
	for _, e := range r.events {
		if e.State == proxy.StatePreflight {
			order = append(order, e.JobID)
		}
	}
	r.evMu.Unlock()
	if strings.Join(order, ",") != strings.Join(ids, ",") {
		t.Fatalf("preflight order %v, want %v", order, ids)
	}
}

func TestSubmitRejectsABadJobID(t *testing.T) {
	r := newRig(t, proxy.Config{})
	job := r.job("/f/x", "x.zip", 10)
	job.ID = "../etc"
	if _, _, err := r.dl.Submit(context.Background(), job); err == nil {
		t.Fatal("a job id that is not 26 base32 characters must be refused")
	}
}

func (r *rig) countEvents(s proxy.State) int {
	r.evMu.Lock()
	defer r.evMu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.State == s {
			n++
		}
	}
	return n
}

// A name finalized earlier by the same Downloader is taken even when the disk cannot see it
// (Game.zip then game.zip on a case-sensitive file system): skip skips, rename renames.
func TestFinalizedNamesCountAsExistingForSkipAndRename(t *testing.T) {
	for policy, want := range map[proxy.OnExists]proxy.State{proxy.OnExistsSkip: proxy.StateSkipped, proxy.OnExistsRename: proxy.StateDone} {
		t.Run(string(policy), func(t *testing.T) {
			r := newRig(t, proxy.Config{OnExists: policy, Concurrency: 1})
			if realfs.IsCaseInsensitive(t, r.fs.Dir()) {
				t.Skip("the disk folds case itself")
			}
			body := fakeproxy.Pattern(100)
			r.srv.Script("/f/a", fakeproxy.OK(body))
			r.srv.Script("/f/b", fakeproxy.OK(body))
			res := r.dl.Run(context.Background(), []proxy.Job{r.job("/f/a", "Game.zip", 100), r.job("/f/b", "game.zip", 100)})
			if res[0].State != proxy.StateDone || res[1].State != want {
				t.Fatalf("states %s, %s; want done, %s", res[0].State, res[1].State, want)
			}
		})
	}
}

// Alicerce residual: history records absolute paths, so DestRoot is required and absolute.
func TestNewRequiresAnAbsoluteDestRoot(t *testing.T) {
	r := newRig(t, proxy.Config{})
	for _, root := range []string{"", "roms", "./roms"} {
		o := proxy.Options{
			HTTP: r.srv.Client(), Clock: r.clock, Sleeper: r.clock, Rand: zeroRand{},
			Staging: r.fs, Dest: r.fs, Finalizer: fakefs.Finalizer{},
			Confine: func(d string) (string, error) { return d, nil },
			History: r.history, Journal: r.journal, DestRoot: root,
			Config: proxy.Config{Platforms: map[string]bool{"snes": true}},
		}
		if _, err := proxy.New(o); err == nil {
			t.Errorf("DestRoot %q must be refused", root)
		}
	}
}
