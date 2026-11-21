package proxy_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeclock"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

type zeroRand struct{}

func (zeroRand) Int63n(int64) int64 { return 0 }

type memHistory struct {
	mu   sync.Mutex
	recs []proxy.Record
}

func (h *memHistory) Append(r proxy.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r)
	return nil
}

// rig wires a downloader to the doubles: one fake FS holding both the staging directory and
// the ROM tree, a fake clock and a scripted proxy.
type rig struct {
	t       *testing.T
	fs      *fakefs.FS
	clock   *fakeclock.Clock
	srv     *fakeproxy.Server
	history *memHistory
	journal *proxy.MemJournal
	dl      *proxy.Downloader
	events  []proxy.Event
	evMu    sync.Mutex
}

func newRig(t *testing.T, cfg proxy.Config) *rig {
	t.Helper()
	r := &rig{t: t, fs: fakefs.New(t), clock: fakeclock.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		history: &memHistory{}, journal: &proxy.MemJournal{}}
	r.srv = fakeproxy.New(t, r.clock)
	if cfg.Platforms == nil {
		cfg.Platforms = map[string]bool{"snes": true, "ps1": true}
	}
	dl, err := proxy.New(proxy.Options{
		HTTP: r.srv.Client(), Clock: r.clock, Sleeper: r.clock, Rand: zeroRand{},
		Staging: r.fs, Dest: r.fs, Finalizer: fakefs.Finalizer{},
		Confine: func(dir string) (string, error) { return dir, r.fs.MkdirAll(dir, 0o700) },
		History: r.history, Journal: r.journal, Config: cfg, DestRoot: r.fs.Dir(),
		OnEvent: func(e proxy.Event) { r.evMu.Lock(); r.events = append(r.events, e); r.evMu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.dl = dl
	return r
}

func (r *rig) job(path, filename string, size int) proxy.Job {
	return proxy.Job{
		Link: mothdex.Link{URL: "https://proxy.mothdex.net" + path, Filename: filename, Size: int64(size), Format: "zip"},
		Slug: "game", Platform: "snes", DestDir: "roms/snes",
	}
}

func (r *rig) download(job proxy.Job) proxy.Result {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return r.dl.Download(ctx, job)
}

func TestDownloadHappyPath(t *testing.T) {
	r := newRig(t, proxy.Config{})
	body := fakeproxy.Pattern(300_000)
	r.srv.Script("/f/a", fakeproxy.OK(body))

	res := r.download(r.job("/f/a", "../../Super Game.sfc", len(body)))

	if res.State != proxy.StateDone || res.Err != nil || res.Name != "Super Game.sfc" || res.Attempts != 1 {
		t.Fatalf("result %+v", res)
	}
	got, err := os.ReadFile(r.fs.Path("roms/snes/Super Game.sfc"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("final file: %v", err)
	}
	sum := sha256.Sum256(body)
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("history must carry the SHA-256 of what was received")
	}
	if entries, _ := os.ReadDir(r.fs.Path("downloads")); len(entries) != 0 {
		t.Fatalf("staging not empty: %v", entries)
	}
	if left, _ := r.journal.Entries(); len(left) != 0 {
		t.Fatalf("journal not cleared: %v", left)
	}
	if len(r.history.recs) != 1 || r.history.recs[0].State != proxy.StateDone {
		t.Fatalf("history %+v", r.history.recs)
	}
	if r.fs.Syncs("roms/snes") == 0 {
		t.Fatal("the destination directory must be flushed before Done")
	}
}

func TestDownloadRetriesATruncatedStreamOnTheSameDescriptor(t *testing.T) {
	r := newRig(t, proxy.Config{})
	body := fakeproxy.Pattern(100_000)
	r.srv.Script("/f/t", fakeproxy.Truncate(body, 40_000), fakeproxy.OK(body))

	res := r.download(r.job("/f/t", "t.zip", len(body)))

	if res.State != proxy.StateDone || res.Attempts != 2 {
		t.Fatalf("result %+v err %v", res, res.Err)
	}
	if got, _ := os.ReadFile(r.fs.Path("roms/snes/t.zip")); string(got) != string(body) {
		t.Fatal("the retry must start from byte 0")
	}
	opens := 0
	for _, c := range r.fs.Calls() {
		if c.Op == fakefs.OpOpenExcl && strings.HasSuffix(c.Name, ".part") {
			opens++
		}
	}
	if opens != 1 {
		t.Fatalf("the staging file was opened %d times, want once (same fd across retries)", opens)
	}
}

func TestDownloadSkipsAnExistingFileWithoutTouchingIt(t *testing.T) {
	r := newRig(t, proxy.Config{})
	if err := os.MkdirAll(r.fs.Path("roms/snes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.fs.Path("roms/snes/x.zip"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.srv.Script("/f/x", fakeproxy.OK(fakeproxy.Pattern(10)))

	res := r.download(r.job("/f/x", "x.zip", 10))

	if res.State != proxy.StateSkipped {
		t.Fatalf("state %s", res.State)
	}
	if got, _ := os.ReadFile(r.fs.Path("roms/snes/x.zip")); string(got) != "mine" {
		t.Fatal("the existing file must be untouched")
	}
	if len(r.srv.Requests()) != 0 {
		t.Fatal("a skipped job must not hit the network")
	}
}

func TestDownloadRejectsAnOffPolicyURLWithoutConnecting(t *testing.T) {
	r := newRig(t, proxy.Config{})
	job := r.job("/f/x", "x.zip", 10)
	job.Link.URL = "http://proxy.mothdex.net/f/x"

	res := r.download(job)

	if res.State != proxy.StateFailed || res.Err == nil || res.Err.Code != proxy.CodeUntrustedURL || r.srv.Connections() != 0 {
		t.Fatalf("result %+v, connections %d", res, r.srv.Connections())
	}
}
