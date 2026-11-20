package mothdex

import (
	"context"
	"net/http"
)

// Search runs POST /search.
func (c *Client) Search(ctx context.Context, r SearchRequest) (SearchResponse, error) {
	const op = "search"
	if err := validateSearch(op, r); err != nil {
		return SearchResponse{}, err
	}
	data, err := c.call(ctx, op, http.MethodPost, "/search", r)
	if err != nil {
		return SearchResponse{}, err
	}
	d, derr := decodeData(op, data, func(d searchData) bool { return d.Results != nil })
	if derr != nil {
		return SearchResponse{}, derr
	}
	return toSearchResponse(d), nil
}

// Entry runs POST /entry. An unknown slug returns an error with KindNotFound.
func (c *Client) Entry(ctx context.Context, slug string) (Entry, error) {
	const op = "entry"
	if err := validateSlug(op, slug); err != nil {
		return Entry{}, err
	}
	return c.fetchEntry(ctx, op, "/entry", struct {
		Slug string `json:"slug"`
	}{slug})
}

// Random runs POST /entry/random (POST only).
func (c *Client) Random(ctx context.Context, f Filters) (Entry, error) {
	const op = "random"
	if err := validateFilters(op, f); err != nil {
		return Entry{}, err
	}
	return c.fetchEntry(ctx, op, "/entry/random", f)
}

func (c *Client) fetchEntry(ctx context.Context, op, path string, body any) (Entry, error) {
	data, err := c.call(ctx, op, http.MethodPost, path, body)
	if err != nil {
		return Entry{}, err
	}
	d, derr := decodeData(op, data, func(d entryData) bool { return d.Entry != nil })
	if derr != nil {
		return Entry{}, derr
	}
	return normalizeEntry(*d.Entry), nil
}

// Platforms runs GET /platforms; the result is sorted by ID.
func (c *Client) Platforms(ctx context.Context) ([]Platform, error) {
	const op = "platforms"
	data, err := c.call(ctx, op, http.MethodGet, "/platforms", nil)
	if err != nil {
		return nil, err
	}
	d, derr := decodeData(op, data, func(d platformsData) bool { return d.Platforms != nil })
	if derr != nil {
		return nil, derr
	}
	return sortedPlatforms(d.Platforms), nil
}

// Regions runs GET /regions; the result is sorted by ID.
func (c *Client) Regions(ctx context.Context) ([]Region, error) {
	const op = "regions"
	data, err := c.call(ctx, op, http.MethodGet, "/regions", nil)
	if err != nil {
		return nil, err
	}
	d, derr := decodeData(op, data, func(d regionsData) bool { return d.Regions != nil })
	if derr != nil {
		return nil, derr
	}
	return sortedRegions(d.Regions), nil
}

// Info runs GET /info.
func (c *Client) Info(ctx context.Context) (DBInfo, error) {
	const op = "info"
	data, err := c.call(ctx, op, http.MethodGet, "/info", nil)
	if err != nil {
		return DBInfo{}, err
	}
	d, derr := decodeData(op, data, func(d infoData) bool { return d.TotalEntries != nil })
	if derr != nil {
		return DBInfo{}, derr
	}
	return DBInfo{TotalEntries: *d.TotalEntries}, nil
}
