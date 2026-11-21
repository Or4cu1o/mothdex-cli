// Package session keeps the last search so that commands can refer to a result by its index
// (app-service.md section 3). It lives in the cache namespace `session`, which is never evicted
// and still works with the cache disabled.
package session

import (
	"errors"
	"fmt"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
)

const key = "last"

// Codes of the not-found errors (cli-output.md error table, exit 3).
const (
	CodeNoSession        = "no_session"
	CodeIndexOutOfRange  = "index_out_of_range"
	maxSlugs             = 1000
	notFoundExit         = 3
	maxSlugBytes         = 256
	maxQueryBytes        = 1024
	maxQueryListElements = 64
)

// Last is the saved search: the query, the page and the slugs of the results in index order.
type Last struct {
	Query Query    `json:"query"`
	Page  int      `json:"page"`
	Slugs []string `json:"slugs"`
}

// Query is the search that produced Last.
type Query struct {
	Text      string   `json:"text"`
	Platforms []string `json:"platforms,omitempty"`
	Regions   []string `json:"regions,omitempty"`
	Serial    string   `json:"serial,omitempty"`
	Sort      string   `json:"sort,omitempty"`
}

// Error is a not-found failure with a stable code.
type Error struct{ code string }

// Code returns no_session or index_out_of_range.
func (e *Error) Code() string { return e.code }

// ExitCode is 3 (not found).
func (e *Error) ExitCode() int { return notFoundExit }

// Error implements error.
func (e *Error) Error() string { return "session: " + e.code }

// Is lets errors.Is match on the code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.code == e.code
}

// Sentinels for errors.Is.
var (
	ErrNoSession       = &Error{CodeNoSession}
	ErrIndexOutOfRange = &Error{CodeIndexOutOfRange}
)

// Store saves and resolves the last search.
type Store struct{ c cache.Store }

// New returns a Store over the cache.
func New(c cache.Store) *Store { return &Store{c: c} }

// Save replaces the last search. The data is bounded: a hostile API answer cannot make the
// session file large.
func (s *Store) Save(l Last) error {
	if len(l.Slugs) > maxSlugs {
		l.Slugs = l.Slugs[:maxSlugs]
	}
	for _, sl := range l.Slugs {
		if len(sl) > maxSlugBytes {
			return errors.New("session: slug too long")
		}
	}
	if len(l.Query.Text) > maxQueryBytes || len(l.Query.Platforms) > maxQueryListElements || len(l.Query.Regions) > maxQueryListElements {
		return errors.New("session: query too large")
	}
	if err := s.c.PutJSON(cache.NSSession, key, l); err != nil {
		return fmt.Errorf("session: save: %w", err)
	}
	return nil
}

// Clear forgets the last search (best effort): after a failed Save the older session must not
// answer <n> for a different search.
func (s *Store) Clear() { _ = s.c.Delete(cache.NSSession, key) }

// Load returns the last search, or ErrNoSession.
func (s *Store) Load() (Last, error) {
	var l Last
	e, err := s.c.GetJSON(cache.NSSession, key, &l)
	if err != nil {
		return Last{}, fmt.Errorf("session: load: %w", err)
	}
	if !e.Hit {
		return Last{}, ErrNoSession
	}
	return l, nil
}

// Resolve maps a 1-based result index of the last search to its slug: ErrNoSession when nothing
// was saved, ErrIndexOutOfRange when the index is not in 1..len(slugs).
func (s *Store) Resolve(index int) (string, error) {
	l, err := s.Load()
	if err != nil {
		return "", err
	}
	if index < 1 || index > len(l.Slugs) {
		return "", ErrIndexOutOfRange
	}
	return l.Slugs[index-1], nil
}
