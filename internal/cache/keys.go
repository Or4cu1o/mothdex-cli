package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const (
	defaultMaxResults = 100
	keyVersion        = 1
)

// KeySearch is the key of a /search response: the SHA-256 of the canonical request.
func KeySearch(baseURL string, r mothdex.SearchRequest) string {
	page, maxResults := r.Page, r.MaxResults
	if page < 1 {
		page = 1
	}
	if maxResults < 1 {
		maxResults = defaultMaxResults
	}
	return hashKey(map[string]any{
		"kind":        "search",
		"search_key":  strings.ToLower(strings.TrimSpace(r.SearchKey)),
		"platforms":   canonicalList(r.Platforms),
		"regions":     canonicalList(r.Regions),
		"serial":      strings.TrimSpace(r.Serial),
		"page":        page,
		"max_results": maxResults,
		"base_url":    baseURL,
	})
}

// KeyEntry is the key of a /entry response.
func KeyEntry(baseURL, slug string) string {
	return hashKey(map[string]any{"kind": "entry", "slug": slug, "base_url": baseURL})
}

// KeyMeta is the key of /platforms or /regions (what is "platforms" or "regions").
func KeyMeta(baseURL, what string) string {
	return hashKey(map[string]any{"kind": "meta", "what": what, "base_url": baseURL})
}

// KeyInfo is the key of /info.
func KeyInfo(baseURL string) string {
	return hashKey(map[string]any{"kind": "info", "base_url": baseURL})
}

// KeyCover is the key of a cover image, derived from its URL.
func KeyCover(coverURL string) string {
	return hashKey(map[string]any{"kind": "cover", "url": coverURL})
}

// canonicalList lower-cases, trims, de-duplicates and sorts; it never returns nil so
// that nil and empty lists hash the same.
func canonicalList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// hashKey hashes a canonical JSON document. encoding/json sorts map keys, so the document
// is canonical by construction.
func hashKey(fields map[string]any) string {
	fields["v"] = keyVersion
	raw, err := json.Marshal(fields)
	if err != nil { // only strings, ints and string slices go in; cannot happen
		panic("cache: canonical key encoding failed: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// keyPattern is the allowlist for keys used as file names: no separators, dots or upper case.
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

// deviceKey rejects Windows device names, which would be devices as file names (con.json).
var deviceKey = regexp.MustCompile(`^(con|prn|aux|nul|(com|lpt)[0-9])$`)

func validKey(key string) error {
	if !keyPattern.MatchString(key) || deviceKey.MatchString(key) {
		return ErrInvalidKey
	}
	return nil
}
