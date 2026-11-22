package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// T-27 / ui.md 2.1: -f of the command being run becomes ConsentAny; without it the consent is 0, so
// the service asks again when only the real size crosses the threshold.
func TestForceBecomesConsentAnyPerCommand(t *testing.T) {
	f := &fake{wait: noop}
	exec(t, f, nil, "download", "1")
	if len(f.reqs) != 1 || f.reqs[0].SizeConsent != 0 {
		t.Fatalf("no -f: %+v", f.reqs)
	}
	exec(t, f, nil, "download", "1", "-f")
	if len(f.reqs) != 1 || f.reqs[0].SizeConsent != app.ConsentAny {
		t.Fatalf("-f: %+v", f.reqs)
	}
}

// A job that stops for confirmation at the real size says so with both sizes and the way out (the
// catalog size when there is one); the exit code is the one of confirmation_required (2).
func TestConfirmationRequiredAtTheRealSize(t *testing.T) {
	run := func(args map[string]any) *result {
		failure := &app.Error{Code: app.CodeConfirmationNeeded, Message: "the real size needs confirmation", Args: args}
		f := &fake{wait: func() (app.BatchResult, error) {
			return app.BatchResult{Failed: 1, Jobs: []app.JobSnapshot{job("a", "failed", failure)}}, nil
		}}
		return exec(t, f, nil, "download", "1")
	}
	r := run(map[string]any{"size": int64(2 << 30), "estimate": int64(5 << 20), "hint_key": "errors.confirmation_required.size_hint"})
	out := r.err.String()
	if r.code != 2 || !strings.Contains(out, "The file is 2.0 GB; the catalog said 5.0 MB.") || !strings.Contains(out, "Add -f") || strings.Contains(out, "much larger") {
		t.Fatalf("exit %d: %q", r.code, out)
	}
}

// The drift note reaches warnings[] of the --json envelope and the job carries catalog_size.
func TestSizeDriftWarningInJSON(t *testing.T) {
	j := job("a", "done", nil)
	j.SizeDrift, j.Expected, j.CatalogSize = true, 5000, 2000
	f := &fake{wait: func() (app.BatchResult, error) { return app.BatchResult{Done: 1, Jobs: []app.JobSnapshot{j}}, nil }}
	r := exec(t, f, nil, "download", "1", "--json")
	var env struct {
		Data struct {
			Jobs []struct {
				CatalogSize int64 `json:"catalog_size"`
			} `json:"jobs"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "5 KB") || !strings.Contains(env.Warnings[0], "2 KB") || env.Data.Jobs[0].CatalogSize != 2000 {
		t.Fatalf("%v %s", err, r.out.String())
	}
}
