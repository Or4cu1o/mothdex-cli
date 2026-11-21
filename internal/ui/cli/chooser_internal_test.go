package cli

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

func TestParseChoice(t *testing.T) {
	choices := []app.LinkChoice{
		{Index: 2, Available: true},
		{Index: 1, Available: true},
		{Index: 3, Available: false},
	}
	tests := []struct {
		answer  string
		choices []app.LinkChoice
		index   int
		kind    choiceAnswer
	}{
		{"", choices, 2, answerPick},
		{"  ", choices, 2, answerPick},
		{"1", choices, 1, answerPick},
		{" 2 ", choices, 2, answerPick},
		{"3", choices, 0, answerInvalid}, // not downloadable
		{"4", choices, 0, answerInvalid},
		{"0", choices, 0, answerInvalid},
		{"-1", choices, 0, answerInvalid},
		{"x", choices, 0, answerInvalid},
		{"q", choices, 0, answerCancel},
		{"Q", choices, 0, answerCancel},
		{"", []app.LinkChoice{{Index: 1, Available: false}}, 0, answerInvalid}, // nothing to default to
	}
	for _, tc := range tests {
		idx, kind := parseChoice(tc.answer, tc.choices)
		if idx != tc.index || kind != tc.kind {
			t.Errorf("parseChoice(%q) = %d, %d; want %d, %d", tc.answer, idx, kind, tc.index, tc.kind)
		}
	}
}

func TestShellNav(t *testing.T) {
	tests := []struct {
		page, of int
		want     []string
	}{
		{1, 0, nil},
		{1, 1, nil},
		{1, 3, []string{"ui.shell.search.next", "ui.shell.search.info", "ui.shell.search.download"}},
		{2, 3, []string{"ui.shell.search.next", "ui.shell.search.prev", "ui.shell.search.info", "ui.shell.search.download"}},
		{3, 3, []string{"ui.shell.search.prev", "ui.shell.search.info", "ui.shell.search.download"}},
	}
	for _, tc := range tests {
		got := shellNav(app.SearchPage{Page: tc.page, TotalPages: tc.of})
		if len(got) != len(tc.want) {
			t.Errorf("shellNav(%d/%d) = %v, want %v", tc.page, tc.of, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("shellNav(%d/%d) = %v, want %v", tc.page, tc.of, got, tc.want)
			}
		}
	}
}

func TestSameSearchIgnoresPagingAndSorting(t *testing.T) {
	base := app.SearchQuery{Text: "mario", Platforms: []string{"nes"}, Regions: []string{"us"}, Page: 1}
	tests := []struct {
		name  string
		other app.SearchQuery
		want  bool
	}{
		{"another page", app.SearchQuery{Text: "mario", Platforms: []string{"nes"}, Regions: []string{"us"}, Page: 3}, true},
		{"another sort", app.SearchQuery{Text: "mario", Platforms: []string{"nes"}, Regions: []string{"us"}, Sort: app.SortSize}, true},
		{"another text", app.SearchQuery{Text: "zelda", Platforms: []string{"nes"}, Regions: []string{"us"}}, false},
		{"another platform", app.SearchQuery{Text: "mario", Platforms: []string{"snes"}, Regions: []string{"us"}}, false},
		{"another region", app.SearchQuery{Text: "mario", Platforms: []string{"nes"}}, false},
		{"another serial", app.SearchQuery{Text: "mario", Platforms: []string{"nes"}, Regions: []string{"us"}, Serial: "X"}, false},
	}
	for _, tc := range tests {
		if got := sameSearch(base, tc.other); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
