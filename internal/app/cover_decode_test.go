package app

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/cover"
)

// DecodeCover refuses with a not_found error whose cause is cover.ErrDecode (gui.md section 8).
func TestDecodeCoverMapsRefusals(t *testing.T) {
	_, err := DecodeCover(bytes.NewReader([]byte("<svg/>")), "image/svg+xml", 0)
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != CodeNotFound || !errors.Is(err, cover.ErrDecode) {
		t.Fatalf("%v", err)
	}
}
