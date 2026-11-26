package log

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// refAlphabet is Crockford base32: no I, L, O or U, so a ref read aloud or retyped stays the same.
const (
	refAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	refLength   = 6
)

var refPattern = regexp.MustCompile(`^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{6}$`)

// ErrRef is returned for text that is not an error ref.
var ErrRef = errors.New("log: not an error ref")

// NewRef returns the id of one error occurrence. It carries no time, process or user, and it is a
// lookup key only, never an authorization (TM-26).
func NewRef() string {
	var b [refLength]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error
	for i := range b {
		b[i] = refAlphabet[int(b[i])%len(refAlphabet)] // 256 is a multiple of 32: no bias
	}
	return string(b[:])
}

// ParseRef normalizes what a person typed: any case, O as 0, I and L as 1, spaces and dashes
// ignored.
func ParseRef(in string) (string, error) {
	s := strings.NewReplacer(" ", "", "-", "", "O", "0", "I", "1", "L", "1").Replace(strings.ToUpper(in))
	if !refPattern.MatchString(s) {
		return "", ErrRef
	}
	return s, nil
}

// ErrorRecord is one shown error as it goes to the log.
type ErrorRecord struct {
	Ref     string
	Code    string
	Cmd     string
	Message string // English
	Cause   error
	Attrs   []slog.Attr // more of the same line, a panic's stack
}

// RecordError writes the line that `logs show --ref` finds. Call it before printing the error.
func RecordError(l *slog.Logger, r ErrorRecord) {
	if l == nil {
		return
	}
	msg := r.Message
	if msg == "" {
		msg = r.Code
	}
	attrs := []slog.Attr{slog.String("ref", r.Ref), slog.String("code", r.Code), slog.String("cmd", r.Cmd)}
	if r.Cause != nil {
		attrs = append(attrs, slog.String("cause", r.Cause.Error()))
	}
	attrs = append(attrs, r.Attrs...)
	l.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
}

// Refs gives each error occurrence one ref: asking again for the same error value returns the same
// ref and writes nothing, so a screen that redraws an error does not log it again. The zero value
// is ready to use.
type Refs struct {
	mu    sync.Mutex
	seen  map[error]string
	order []error // oldest first
}

// maxRefs bounds what a long Shell or TUI run remembers; the oldest errors are off the screen.
const maxRefs = 1000

// Of returns the ref of key, recording rec on the first call. key must be comparable (a pointer
// error type).
func (r *Refs) Of(l *slog.Logger, key error, rec ErrorRecord) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ref, ok := r.seen[key]; ok {
		return ref
	}
	if r.seen == nil {
		r.seen = map[error]string{}
	}
	if len(r.order) == maxRefs {
		delete(r.seen, r.order[0])
		r.order = slices.Delete(r.order, 0, 1)
	}
	rec.Ref = NewRef()
	r.seen[key] = rec.Ref
	r.order = append(r.order, key)
	RecordError(l, rec)
	return rec.Ref
}
