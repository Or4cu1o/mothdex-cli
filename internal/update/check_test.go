package update

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckCacheAndNotice(t *testing.T) {
	u, _ := fixture(t)
	u.o.DataDir = t.TempDir()
	now := u.o.Now()
	u.o.Now = func() time.Time { return now }
	n := serve(t, u, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", origin+"/tag/v2.0.0")
		w.WriteHeader(302)
	})
	result, err := u.Check(context.Background(), false)
	if err != nil || !result.Notify || !result.Available || n.Load() != 1 {
		t.Fatalf("initial check: %+v %v %d", result, err, n.Load())
	}
	u.MarkNotified(result.Latest)
	result, err = u.Check(context.Background(), false)
	if err != nil || result.Notify || !result.FromCache || n.Load() != 1 {
		t.Fatalf("cache: %+v %v %d", result, err, n.Load())
	}
	now = now.Add(24 * time.Hour)
	result, err = u.Check(context.Background(), false)
	if err != nil || !result.Notify || result.FromCache || n.Load() != 2 {
		t.Fatalf("next day: %+v %v %d", result, err, n.Load())
	}
	if _, err = u.Check(context.Background(), true); err != nil || n.Load() != 3 {
		t.Fatalf("explicit check did not rediscover: %v %d", err, n.Load())
	}
}
func TestOffSwitchesMakeZeroRequests(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, reason string
		enabled                  bool
	}{
		{"config", "", "", "config", false}, {"env", "MOTHDEX_NO_UPDATE_CHECK", "1", "env", true}, {"env arbitrary", "MOTHDEX_NO_UPDATE_CHECK", "yes", "env", true}, {"ci", "CI", "false", "ci", true}, {"unmanaged", "", "", "unmanaged", true}, {"dev", "", "", "dev_build", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			n := serve(t, u, func(_ http.ResponseWriter, _ *http.Request) { t.Error("opt-out made request") })
			if tc.name == "unmanaged" {
				_ = os.Remove(filepath.Join(s.Dir, stateName))
			}
			if tc.name == "dev" {
				u.o.Current = ""
			}
			getenv := func(key string) string {
				if key == tc.key {
					return tc.value
				}
				return ""
			}
			reason := u.SkipReason(tc.enabled, getenv)
			if reason != tc.reason {
				t.Fatalf("got %q", reason)
			}
			if reason == "" {
				_, _ = u.Check(context.Background(), false)
			}
			if n.Load() != 0 {
				t.Fatal("opt-out requested network")
			}
		})
	}
}
func TestCacheRejectsInjectionAndLinks(t *testing.T) {
	u, _ := fixture(t)
	u.o.DataDir = t.TempDir()
	p := filepath.Join(u.o.DataDir, "update-check.json")
	for _, value := range []string{`{"schema_version":1,"current":"1.0.0","latest":"9.9.9\u001b[31m","channel":"stable","checked_at":"2026-11-26T15:00:00Z"}`, `{} {}`, `{"unknown":1}`} {
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if r := u.Cached(); r.Notify || r.Latest != "" {
			t.Fatalf("printed corrupt cache: %+v", r)
		}
	}
}
