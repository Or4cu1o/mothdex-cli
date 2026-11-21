package proxy_test

import (
	"bytes"
	"net/http"
	"os"
	"runtime"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
)

// Sentinela m2: the name of the staging file is swapped for an attacker's file while the job is
// Finalizing; the placed file must be the downloaded one, or nothing at all.
func TestSwapAtFinalizingNeverPlacesUnverifiedContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an open file cannot be unlinked on Windows, so the swap cannot happen")
	}
	for _, crossDevice := range []bool{false, true} {
		name := map[bool]string{false: "staged in the destination", true: "staged in paths.temp"}[crossDevice]
		t.Run(name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			if crossDevice {
				r.fs.SetVolume("downloads", 1<<50, 2)
			}
			evil := []byte("attacker content")
			swapped := false
			r.rebuild(proxy.Config{}, func(o *proxy.Options) {
				inner := o.OnEvent
				o.OnEvent = func(e proxy.Event) {
					inner(e)
					if e.State != proxy.StateFinalizing || swapped {
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
					realfs.WriteFile(t, p, evil)
				}
			})
			body := fakeproxy.Pattern(20_000)
			r.srv.Script("/f/a", fakeproxy.OK(body))

			res := r.download(r.job("/f/a", "a.zip", len(body)))

			if !swapped {
				t.Fatal("the Finalizing state was never reached")
			}
			got, err := os.ReadFile(r.fs.Path("roms/snes/a.zip")) //nolint:gosec // test path
			switch {
			case err == nil && bytes.Equal(got, evil):
				t.Fatal("the attacker's file was placed under the final name")
			case err == nil && res.State != proxy.StateDone:
				t.Fatalf("state %s but a final file exists", res.State)
			case err == nil && !bytes.Equal(got, body):
				t.Fatal("Done with the wrong content")
			}
		})
	}
}

// Sentinela m12: every step of the no-replace fallback chain runs: Link when the no-replace
// rename is unsupported, then an O_EXCL placeholder when hard links are unsupported too.
func TestNoReplaceFallbackChain(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*fakefs.FS)
		wantOp  string
	}{
		{"hard link", func(f *fakefs.FS) { f.Fail(fakefs.OpRename, "*", fakefs.ErrNoReplaceUnsupported, 1) }, fakefs.OpLink},
		{"placeholder", func(f *fakefs.FS) {
			f.Fail(fakefs.OpRename, "*", fakefs.ErrNoReplaceUnsupported, 1)
			f.Fail(fakefs.OpLink, "*", fakefs.ErrHardLinkUnsupported, 0)
		}, fakefs.OpOpenExcl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			tt.prepare(r.fs)
			body := fakeproxy.Pattern(3000)
			r.srv.Script("/f/a", fakeproxy.OK(body))

			res := r.download(r.job("/f/a", "a.zip", len(body)))

			if res.State != proxy.StateDone {
				t.Fatalf("state %s err %v", res.State, res.Err)
			}
			if got, _ := os.ReadFile(r.fs.Path("roms/snes/a.zip")); !bytes.Equal(got, body) { //nolint:gosec // test path
				t.Fatal("wrong content")
			}
			if names := realfs.Names(t, r.fs.Path("roms/snes")); len(names) != 1 {
				t.Fatalf("leftovers next to the ROM: %v", names)
			}
			seen := false
			for _, c := range r.fs.Calls() {
				seen = seen || (c.Op == tt.wantOp && (c.Name == "roms/snes/a.zip" || c.To == "roms/snes/a.zip"))
			}
			if !seen {
				t.Fatalf("the %s step of the chain was not used: %v", tt.name, r.fs.Calls())
			}
		})
	}
}

// Sentinela m5: case 27 with the redirect policy of the real download client: a 302 comes back
// to the downloader, which fails the job as untrusted_url without following it.
func TestRealClientRedirectPolicyFailsTheJob(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.rebuild(proxy.Config{}, func(o *proxy.Options) {
		o.HTTP = &http.Client{Transport: r.srv.Transport(), CheckRedirect: proxy.NewHTTPClient().CheckRedirect}
	})
	r.srv.Script("/f/a", fakeproxy.Redirect(http.StatusFound, "https://evil.test/x"))

	res := r.download(r.job("/f/a", "a.zip", 10))

	wantFailed(t, res, proxy.CodeUntrustedURL)
	if n := len(r.srv.Requests()); n != 1 {
		t.Fatalf("%d requests, the redirect must not be followed or retried", n)
	}
}
