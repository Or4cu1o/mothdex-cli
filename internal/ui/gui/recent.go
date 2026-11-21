package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

const (
	stateFile = "tui_state" // shared with the TUI (docs/ux/gui.md section 5.2)
	maxRecent = 5
)

// readRecent reads the recent queries from `<paths.data>/tui_state`, the file the TUI writes. A missing or
// unreadable file is an empty list; each query is cleaned of control characters.
func readRecent(dataDir string) []string {
	if dataDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dataDir, stateFile)) //nolint:gosec // the UI's own file under paths.data
	if err != nil {
		return nil
	}
	var st struct {
		Recent []string `json:"recent"`
	}
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	out := make([]string, 0, maxRecent)
	for _, r := range st.Recent {
		if r = strings.TrimSpace(app.SanitizeText(r)); r != "" && len(out) < maxRecent {
			out = append(out, r)
		}
	}
	return out
}
