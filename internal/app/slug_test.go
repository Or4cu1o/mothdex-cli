package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// api-client section 2: a list drops unsafe slugs before numbering (Dropped counts them), a single
// entry answer fails with api_incompatible, Random skips them.
func TestUnsafeSlugsFromTheAPI(t *testing.T) {
	ctx := context.Background()
	bad := mothdex.Entry{Slug: "../x", Title: "Bad", Platform: "snes"}
	c := &fakeClient{search: mothdex.SearchResponse{Results: append(entries("a", "b"), bad, mothdex.Entry{Slug: "Caps"}), CurrentPage: 1, TotalPages: 1, TotalResults: 4}}
	s := newSvc(t, c)
	page, err := s.Search(ctx, SearchQuery{Text: "x"})
	if err != nil || page.Dropped != 2 || len(page.Results) != 2 || page.Results[1].Index != 2 {
		t.Fatalf("%+v %v", page, err)
	}
	c.entries = map[string]mothdex.Entry{"weird": bad}
	if _, err := s.Entry(ctx, Ref{Slug: "weird"}); code(err) != CodeAPIIncompatible {
		t.Fatalf("single entry: %v", err)
	}
	c.randoms = []mothdex.Entry{bad, entries("ok")[0]}
	res, err := s.Random(ctx, RandomQuery{Count: 1})
	if err != nil || len(res.Value) != 1 || res.Value[0].Slug != "ok" {
		t.Fatalf("%+v %v", res, err)
	}
	c.randoms, c.nRandom = []mothdex.Entry{bad, bad}, 0
	if _, err := s.Random(ctx, RandomQuery{Count: 1}); code(err) != CodeAPIIncompatible {
		t.Fatalf("only unsafe answers: %v", err)
	}
}

// F5: a missing slug names what was looked for instead of repeating the code.
func TestEntryNotFoundNamesTheSlug(t *testing.T) {
	s := newSvc(t, &fakeClient{})
	_, err := s.Entry(context.Background(), Ref{Slug: "nope"})
	var ae *Error
	if code(err) != CodeNotFound || !errors.As(err, &ae) || ae.Args["ref"] != "nope" || ae.Message == "not_found" {
		t.Fatalf("%v", err)
	}
}
