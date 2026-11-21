package mothdex

import (
	"fmt"
	"regexp"
)

// Local limits from the contract (section 2).
const (
	maxSlugBytes  = 256
	maxResultsCap = 100
	maxFilterList = 50
)

func invalid(op, msg string) *Error {
	return &Error{Kind: KindInvalidRequest, Op: op, Message: msg}
}

// slugRule is the one slug shape both clients pass on (api-client section 2).
var slugRule = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,255}$`)

// ValidSlug reports whether s is a safe slug: ASCII lowercase letters, digits and "-", starting with a
// letter or digit, 1 to 256 bytes. Slugs end up in command lines, file names, CSV cells and terminals.
func ValidSlug(s string) bool { return slugRule.MatchString(s) }

func validateSlug(op, slug string) *Error {
	if slug == "" {
		return invalid(op, "slug is required")
	}
	if len(slug) > maxSlugBytes {
		return invalid(op, fmt.Sprintf("slug longer than %d bytes", maxSlugBytes))
	}
	if !ValidSlug(slug) {
		return invalid(op, "slug must be lowercase letters, digits and '-', starting with a letter or digit")
	}
	return nil
}

func validateFilters(op string, f Filters) *Error {
	if len(f.Platforms) > maxFilterList {
		return invalid(op, fmt.Sprintf("more than %d platforms", maxFilterList))
	}
	if len(f.Regions) > maxFilterList {
		return invalid(op, fmt.Sprintf("more than %d regions", maxFilterList))
	}
	return nil
}

func validateSearch(op string, r SearchRequest) *Error {
	if r.MaxResults < 0 || r.MaxResults > maxResultsCap {
		return invalid(op, fmt.Sprintf("max_results must be 0..%d", maxResultsCap))
	}
	if r.Page < 0 {
		return invalid(op, "page must be >= 0")
	}
	return validateFilters(op, r.Filters)
}
