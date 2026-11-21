package history

import (
	"bytes"
	"strings"
	"testing"
)

// Sentinela: the history JSON export escapes C1 controls like --json does (U+009B and U+009D act as
// escape sequences on xterm-class terminals).
func TestExportJSONEscapesC1Controls(t *testing.T) {
	var b bytes.Buffer
	rec := Record{ID: "1", Title: "A" + string(rune(0x9b)) + "31mB" + string(rune(0x9d)) + "C", State: "done"}
	if err := Export(&b, "json", []Record{rec}, Meta{}); err != nil {
		t.Fatal(err)
	}
	for _, c1 := range []rune{0x9b, 0x9d} {
		if strings.ContainsRune(b.String(), c1) {
			t.Fatalf("raw %U in the export", c1)
		}
	}
	if !strings.Contains(b.String(), `\u009b`) {
		t.Fatal(b.String())
	}
}
