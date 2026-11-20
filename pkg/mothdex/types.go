package mothdex

import "sort"

// Entry describes one database item. Unknown JSON fields are ignored.
type Entry struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Platform string   `json:"platform"`
	Regions  []string `json:"regions"`
	Serial   string   `json:"serial"`    // "" when null
	CoverURL string   `json:"cover_url"` // "" when null; external URL, untrusted
	Links    []Link   `json:"links"`
}

// Link is one download link of an Entry. Every string is external input.
type Link struct {
	Type       string   `json:"type"`
	Extras     []string `json:"extras"`
	Format     string   `json:"format"`
	Filename   string   `json:"filename"` // never use as a path without sanitization
	URL        string   `json:"url"`      // host is checked by the downloader, not here
	Size       int64    `json:"size"`     // bytes; authoritative expected size
	SizeStr    string   `json:"size_str"`
	SourceName string   `json:"source_name"`
	SourceURL  string   `json:"source_url"`
	Hashes     *Hashes  `json:"hashes,omitempty"` // future upstream field; nil today
}

// Platform is one platform; ID comes from the key of the API map.
type Platform struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Brand string `json:"brand"`
	Class string `json:"class"`
}

// Region is one region; ID comes from the key of the API map.
type Region struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DBInfo is the answer of GET /info.
type DBInfo struct {
	TotalEntries int64 `json:"total_entries"`
}

// Filters are shared by Search and Random.
type Filters struct {
	SearchKey string   `json:"search_key,omitempty"`
	Platforms []string `json:"platforms,omitempty"`
	Regions   []string `json:"regions,omitempty"`
	Serial    string   `json:"serial,omitempty"` // exact match
}

// SearchRequest is the body of POST /search.
type SearchRequest struct {
	Filters
	MaxResults int `json:"max_results,omitempty"` // 0 = API default (100); valid 1..100
	Page       int `json:"page,omitempty"`        // 0 = 1
}

// SearchResponse is returned raw: the API may answer page 0 of 0 when nothing
// matches and clamps a page past the last one. The app service normalizes.
type SearchResponse struct {
	Results        []Entry `json:"results"`
	CurrentResults int     `json:"current_results"`
	TotalResults   int     `json:"total_results"`
	CurrentPage    int     `json:"current_page"`
	TotalPages     int     `json:"total_pages"`
}

// normalizeEntry returns a copy where null lists became empty slices.
func normalizeEntry(e Entry) Entry {
	out := e
	out.Regions = nonNil(e.Regions)
	out.Links = make([]Link, len(e.Links))
	for i, l := range e.Links {
		l.Extras = nonNil(l.Extras)
		l.Hashes = normalizeHashes(l.Hashes)
		out.Links[i] = l
	}
	return out
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func sortedPlatforms(in map[string]platformWire) []Platform {
	out := make([]Platform, 0, len(in))
	for id, p := range in {
		out = append(out, Platform{ID: id, Name: p.Name, Brand: p.Brand, Class: p.Class})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedRegions(in map[string]string) []Region {
	out := make([]Region, 0, len(in))
	for id, name := range in {
		out = append(out, Region{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
