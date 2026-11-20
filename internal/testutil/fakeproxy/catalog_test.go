package fakeproxy_test

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// The T-16 section 8 list has exactly 52 numbered cases; a dropped or
// duplicated entry would silently lose coverage.
func TestCatalogIsComplete(t *testing.T) {
	cases := fakeproxy.Catalog()
	if len(cases) != 52 {
		t.Fatalf("catalog has %d cases, want 52", len(cases))
	}
	for i, c := range cases {
		if c.ID != i+1 {
			t.Errorf("entry %d has ID %d", i+1, c.ID)
		}
		if c.Group == "" || c.Title == "" || c.Needs == "" || c.Assert == "" {
			t.Errorf("case %d has an empty field", c.ID)
		}
		if c.Status == fakeproxy.Covered && c.Test == "" {
			t.Errorf("case %d is covered but names no test", c.ID)
		}
	}
}
