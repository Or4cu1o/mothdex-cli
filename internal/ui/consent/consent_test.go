package consent

import (
	"fmt"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
)

func stopped(state app.State, code string, args map[string]any, catalog int64) app.JobSnapshot {
	return app.JobSnapshot{State: state, CatalogSize: catalog, Expected: 40, Err: &app.Error{Code: code, Args: args}}
}

func TestNeededSizeAndConsent(t *testing.T) {
	const real = int64(700 << 20)
	tests := []struct {
		name      string
		job       app.JobSnapshot
		needed    bool
		size      int64
		suffix    string
		estimate  string
		wantTexts bool
	}{
		{"stopped with a catalog size", stopped(app.StateFailed, "confirmation_required", map[string]any{"size": real}, 40), true, real, "", "S40", true},
		{"stopped, the catalog had no size", stopped(app.StateFailed, "confirmation_required", map[string]any{"size": real}, 0), true, real, "_unknown", "", true},
		{"another failure", stopped(app.StateFailed, "network", map[string]any{"size": real}, 40), false, real, "", "S40", false},
		{"not failed", stopped(app.StateCanceled, "confirmation_required", map[string]any{"size": real}, 40), false, real, "", "S40", false},
		// The catalog's size (Expected) is not what the downloader stopped on: never offered as the consent.
		{"the error has no size", stopped(app.StateFailed, "confirmation_required", nil, 40), false, 0, "", "S40", false},
		{"a size of the wrong type", stopped(app.StateFailed, "confirmation_required", map[string]any{"size": "700"}, 40), false, 0, "", "S40", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Needed(tc.job); got != tc.needed {
				t.Errorf("Needed = %v, want %v", got, tc.needed)
			}
			if got := Size(tc.job); got != tc.size {
				t.Errorf("Size = %d, want %d", got, tc.size)
			}
			if !tc.wantTexts {
				return
			}
			suffix, args := Args(tc.job, func(n int64) string { return fmt.Sprintf("S%d", n) })
			if suffix != tc.suffix || args["estimate"] == nil && tc.estimate != "" || args["estimate"] != nil && args["estimate"] != tc.estimate {
				t.Errorf("Args = %q %v, want suffix %q estimate %q", suffix, args, tc.suffix, tc.estimate)
			}
			if got := args["size"]; got != fmt.Sprintf("S%d", tc.size) {
				t.Errorf("size arg %v", got)
			}
			if req := WithConsent(app.DownloadRequest{}, tc.job); req.SizeConsent != tc.size {
				t.Errorf("SizeConsent %d, want %d", req.SizeConsent, tc.size)
			}
		})
	}
}

func TestRecordNeededAndRecordJob(t *testing.T) {
	const real = int64(700 << 20)
	rec := func(state, code string, cl int64) app.HistoryRecord {
		r := app.HistoryRecord{ID: "j1", Slug: "zelda", Title: "Zelda", Platform: "gbc", LinkIndex: 2, State: state, ContentLength: cl, CatalogSize: 40 << 20}
		if code != "" {
			r.Error = &history.Failure{Code: code, Message: "m"}
		}
		return r
	}
	for _, tc := range []struct {
		name string
		r    app.HistoryRecord
		want bool
	}{
		{"stopped on its size", rec("failed", "confirmation_required", real), true},
		{"another failure", rec("failed", "network", real), false},
		{"done", rec("done", "", real), false},
		{"no Content-Length was reported", rec("failed", "confirmation_required", 0), false},
	} {
		if got := RecordNeeded(tc.r); got != tc.want {
			t.Errorf("%s: RecordNeeded = %v", tc.name, got)
		}
	}
	j := RecordJob(rec("failed", "confirmation_required", real), "/roms/gbc")
	if !Needed(j) || Size(j) != real || Dir(j) != "/roms/gbc" || j.Slug != "zelda" || j.LinkIndex != 2 || j.CatalogSize != 40<<20 {
		t.Errorf("the record as a job: %+v", j)
	}
}
