package mothdex

import (
	"encoding/json"
	"errors"
)

// Wire shapes of the "data" object. Pointer fields detect a missing key.
type (
	platformWire struct {
		Name  string `json:"name"`
		Brand string `json:"brand"`
		Class string `json:"class"`
	}
	searchData struct {
		Results        *[]Entry `json:"results"`
		CurrentResults int      `json:"current_results"`
		TotalResults   int      `json:"total_results"`
		CurrentPage    int      `json:"current_page"`
		TotalPages     int      `json:"total_pages"`
	}
	entryData struct {
		Entry *Entry `json:"entry"`
	}
	platformsData struct {
		Platforms map[string]platformWire `json:"platforms"`
	}
	regionsData struct {
		Regions map[string]string `json:"regions"`
	}
	infoData struct {
		TotalEntries *int64 `json:"total_entries"`
	}
)

var errMissingKey = errors.New("expected key missing from data")

// decodeData unmarshals data into T; a missing expected key is a KindDecode error.
func decodeData[T any](op string, data json.RawMessage, complete func(T) bool) (T, *Error) {
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return out, &Error{Kind: KindDecode, Op: op, HTTPStatus: 200, Message: "unexpected data shape", Err: err}
	}
	if !complete(out) {
		return out, &Error{Kind: KindDecode, Op: op, HTTPStatus: 200, Message: errMissingKey.Error(), Err: errMissingKey}
	}
	return out, nil
}

func toSearchResponse(d searchData) SearchResponse {
	entries := make([]Entry, len(*d.Results))
	for i, e := range *d.Results {
		entries[i] = normalizeEntry(e)
	}
	return SearchResponse{
		Results:        entries,
		CurrentResults: d.CurrentResults,
		TotalResults:   d.TotalResults,
		CurrentPage:    d.CurrentPage,
		TotalPages:     d.TotalPages,
	}
}
