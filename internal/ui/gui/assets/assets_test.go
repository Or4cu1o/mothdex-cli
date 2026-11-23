package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The pins of PINS.md: the embedded files must be exactly the ones that were reviewed.
func TestEmbeddedAssetsMatchTheirPins(t *testing.T) {
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	tests := []struct {
		name string
		data []byte
		sha  string
		size int
	}{
		{"NotoSans-Regular-subset.ttf", NotoSans, "ac3fc986ac93d2aeac30103eff750b87611da0f3460d1ddce4a215c1bf5cdf01", 59800},
		{"Handjet-Regular-subset.ttf", Handjet, "2ba7ebeb0d6cd9a7e962b6cbf608e04a28c799564422144099bec4c306fd057c", 54900},
		{"moth-wings-down.png", MothWingsDown, "a418676ca1027c283f2caa3d08c4879ddbfb3f3667bf7ef568cbf804368cf953", 231},
		{"moth-wings-up.png", MothWingsUp, "396e2413e23030ca4ff7bf093e7147bcbb4bc094cb40c7772c7aa83b1a19e87c", 207},
		{"mothdex-icon-256.png", AppIcon, "17f7f920da00659261a59786b703df2ac497dc96caceffb2bf84112cd80b3c0f", 1490},
		{"gamecontrollerdb.txt", []byte(ControllerDB), "a9994e85b0d608cfcb4df0e1424523e488829acb1017b6a2a2adf8df6478cb26", 614810},
	}
	for _, tc := range tests {
		if got := sum(tc.data); got != tc.sha || len(tc.data) != tc.size {
			t.Errorf("%s: sha256 %s and %d bytes, pinned %s and %d bytes", tc.name, got, len(tc.data), tc.sha, tc.size)
		}
	}
}

func TestTheFontIsATrueTypeFileAndTheDatabaseHasMappings(t *testing.T) {
	if len(NotoSans) < 4 || string(NotoSans[:4]) != "\x00\x01\x00\x00" {
		t.Fatal("the font does not start with the TrueType signature")
	}
	if !strings.Contains(ControllerDB, "platform:Linux") || !strings.Contains(ControllerDB, "platform:Windows") {
		t.Fatal("the database lacks Linux or Windows mappings")
	}
}
