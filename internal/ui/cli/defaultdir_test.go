package cli_test

import (
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// T-26: the hint for a missing default folder names it and the two ways out.
func TestMissingDefaultFolderHint(t *testing.T) {
	f := &fake{
		links: func(app.Ref, app.LinkSelector) ([]app.LinkChoice, error) {
			return nil, &app.Error{Code: "path_rejected", Message: "the default download folder does not exist",
				Args: map[string]any{"path": "/roms", "hint_key": "errors.path_rejected.missing_hint"}}
		},
	}
	r := exec(t, f, nil, "download", "a")
	out := r.err.String()
	if r.code != 6 || !strings.Contains(out, "/roms does not exist") || !strings.Contains(out, "-o <folder>") || !strings.Contains(out, "config set download.dir") {
		t.Fatalf("exit %d: %q", r.code, out)
	}
}
