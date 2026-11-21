package shell

import (
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// allPlatforms is the prompt word for a search with no platform filter.
const allPlatforms = "all"

// promptText is `mothdex> ` with no search yet, and `mothdex[<platform|all>:<n>]> ` after one,
// where n is the number of results on the page. It is plain text: the line editor refuses a prompt
// with control characters, so it cannot be dimmed (a deviation from shell.md section 2). Text from the user's own
// query is cleaned for the same reason.
func promptText(page app.SearchPage, haveSearch bool) string {
	if !haveSearch {
		return "mothdex> "
	}
	platform := allPlatforms
	if len(page.Query.Platforms) > 0 {
		platform = app.SanitizeText(strings.Join(page.Query.Platforms, ","))
	}
	return "mothdex[" + platform + ":" + strconv.Itoa(len(page.Results)) + "]> "
}
