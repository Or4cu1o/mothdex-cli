package cache

import (
	"regexp"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const base = "https://api.mothdex.net/v1"

func TestKeySearchCanonicalEquivalence(t *testing.T) {
	ref := mothdex.SearchRequest{
		Filters:    mothdex.Filters{SearchKey: "doom", Platforms: []string{"dos", "gba"}, Regions: []string{"eu", "us"}},
		MaxResults: 100, Page: 1,
	}
	same := map[string]mothdex.SearchRequest{
		"case and spaces":   {Filters: mothdex.Filters{SearchKey: "  DoOm ", Platforms: []string{"DOS", "GBA"}, Regions: []string{"EU", "US"}}, MaxResults: 100, Page: 1},
		"order":             {Filters: mothdex.Filters{SearchKey: "doom", Platforms: []string{"gba", "dos"}, Regions: []string{"us", "eu"}}, MaxResults: 100, Page: 1},
		"duplicates":        {Filters: mothdex.Filters{SearchKey: "doom", Platforms: []string{"dos", "gba", "dos"}, Regions: []string{"eu", "us", "EU"}}, MaxResults: 100, Page: 1},
		"explicit defaults": {Filters: mothdex.Filters{SearchKey: "doom", Platforms: []string{"dos", "gba"}, Regions: []string{"eu", "us"}}},
	}
	want := KeySearch(base, ref)
	for name, r := range same {
		if got := KeySearch(base, r); got != want {
			t.Errorf("%s: key differs", name)
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(want) {
		t.Fatalf("key %q is not lowercase hex SHA-256", want)
	}
	empty := mothdex.SearchRequest{Filters: mothdex.Filters{Platforms: []string{}}, Page: 1, MaxResults: 100}
	if KeySearch(base, mothdex.SearchRequest{}) != KeySearch(base, empty) {
		t.Error("nil and empty lists must hash the same")
	}
}

func TestKeySearchDistinguishesRequests(t *testing.T) {
	ref := mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "doom"}}
	different := map[string]string{
		"text":     KeySearch(base, mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "quake"}}),
		"platform": KeySearch(base, mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "doom", Platforms: []string{"dos"}}}),
		"serial":   KeySearch(base, mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "doom", Serial: "AGYP"}}),
		"page":     KeySearch(base, mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "doom"}, Page: 2}),
		"size":     KeySearch(base, mothdex.SearchRequest{Filters: mothdex.Filters{SearchKey: "doom"}, MaxResults: 10}),
		"base URL": KeySearch("http://127.0.0.1:1/v1", ref),
	}
	want := KeySearch(base, ref)
	for name, got := range different {
		if got == want {
			t.Errorf("%s must change the key", name)
		}
	}
}

func TestOtherKeysDoNotCollide(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range []string{
		KeyEntry(base, "doom"), KeyEntry(base, "quake"), KeyMeta(base, "platforms"), KeyMeta(base, "regions"),
		KeyInfo(base), KeyCover("https://img.example/a.jpg"), KeyCover("https://img.example/b.jpg"), KeyEntry("other", "doom"),
	} {
		if keys[k] || validKey(k) != nil {
			t.Fatalf("duplicate or invalid key %q", k)
		}
		keys[k] = true
	}
}

func TestValidKey(t *testing.T) {
	for _, ok := range []string{"abc123", "cooldown", "a-b_c", KeyInfo(base)} {
		if validKey(ok) != nil {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", `a\b`, "A", "a.b", ".hidden", "a b", "a\x00", "con", "nul", "com1", "lpt9", "aux"} {
		if validKey(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
