package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

func TestSplitLine(t *testing.T) {
	tests := []struct {
		name, line string
		want       []string
		errKey     string
	}{
		{"plain", `search zelda -p snes`, []string{"search", "zelda", "-p", "snes"}, ""},
		{"double quotes group", `search "super mario"`, []string{"search", "super mario"}, ""},
		{"single quotes group", `search 'a  b'`, []string{"search", "a  b"}, ""},
		{"backslash is a normal character", `download 1 -o C:\roms`, []string{"download", "1", "-o", `C:\roms`}, ""},
		{"empty quotes are a word", `search ""`, []string{"search", ""}, ""},
		{"tabs and runs of spaces", "  a \t b  ", []string{"a", "b"}, ""},
		{"blank", "   ", nil, ""},
		{"open quote", `search "mario`, nil, "ui.shell.bad_quote"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitLine(tc.line)
			var ke *keyError
			if tc.errKey != "" {
				if !errors.As(err, &ke) || ke.key != tc.errKey {
					t.Fatalf("err = %v, want key %s", err, tc.errKey)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestExpandIndexes(t *testing.T) {
	takes := func(f string) bool { return f == "-o" || f == "--link" }
	tests := []struct {
		name   string
		args   []string
		want   []string
		errKey string
	}{
		{"several numbers stay", []string{"1", "3", "5"}, []string{"1", "3", "5"}, ""},
		{"a range expands", []string{"2-4"}, []string{"2", "3", "4"}, ""},
		{"ranges and numbers mix", []string{"1", "3-4", "9"}, []string{"1", "3", "4", "9"}, ""},
		{"a descending range is refused", []string{"4-2"}, nil, "ui.shell.bad_range"},
		{"a range from zero is refused", []string{"0-3"}, nil, "ui.shell.bad_range"},
		{"a bare zero is left to the command", []string{"0"}, []string{"0"}, ""},
		{"a slug passes through", []string{"a"}, []string{"a"}, ""},
		{"a slug with digits and dashes passes through", []string{"10-yard-fight-nes"}, []string{"10-yard-fight-nes"}, ""},
		{"a range past the page is the service's to judge", []string{"1-9"}, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, ""},
		{"a flag value that looks like a range is not one", []string{"1", "-o", "2020-21"}, []string{"1", "-o", "2020-21"}, ""},
		{"over the cap", []string{"1-101"}, nil, "ui.shell.range_too_big"},
		{"a number that overflows is no range", []string{"99999999999999999999-99999999999999999999"}, nil, "ui.shell.bad_range"},
		{"the largest int cannot wrap the cap", []string{"x", "1-9223372036854775807"}, nil, "ui.shell.range_too_big"},
		{"a range at the largest int", []string{"9223372036854775806-9223372036854775807"}, []string{"9223372036854775806", "9223372036854775807"}, ""},
		{"the cap counts the whole line", []string{"1-60", "61-120"}, nil, "ui.shell.range_too_big"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandIndexes(tc.args, takes)
			var ke *keyError
			if tc.errKey != "" {
				if !errors.As(err, &ke) || ke.key != tc.errKey {
					t.Fatalf("err = %v, want key %s", err, tc.errKey)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandIndexesAllowsExactlyTheCap(t *testing.T) {
	got, err := expandIndexes([]string{"1-100"}, nil)
	if err != nil || len(got) != maxIndexes || got[0] != "1" || got[len(got)-1] != strconv.Itoa(maxIndexes) {
		t.Fatalf("got %d entries, %v", len(got), err)
	}
}

func TestRecordable(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"search zelda", true},
		{" search zelda", false},
		{"\tsearch zelda", false},
		{"", false},
		{"config set interface.color never", true},
		{"config set api.token abc", false},
		{"config set API.KEY abc", false},
		{"config get api.token", true},
		{"config set api.base_url https://example.test", false},
		{"config set API.BASE_URL x", false},
		{"config set download.dir https://user:pass@example.test/p", false},
		{"config set download.dir /tmp/roms", true},
		{"search https://user:pass@example.test/x", true}, // only config set is judged
	}
	for _, tc := range tests {
		if got := recordable(tc.line); got != tc.want {
			t.Errorf("recordable(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestRecordCollapsesDuplicatesAndKeepsTheLast1000(t *testing.T) {
	var lines []string
	for i := 0; i < historyMax+50; i++ {
		lines = record(lines, "cmd "+strconv.Itoa(i))
		lines = record(lines, "cmd "+strconv.Itoa(i)) // consecutive duplicate
	}
	if len(lines) != historyMax || lines[0] != "cmd 50" || lines[len(lines)-1] != "cmd "+strconv.Itoa(historyMax+49) {
		t.Fatalf("len %d first %q last %q", len(lines), lines[0], lines[len(lines)-1])
	}
	before := []string{"a"}
	if got := record(before, "b"); len(before) != 1 || len(got) != 2 {
		t.Fatal("record must return a new slice and leave its input alone")
	}
}

func TestClosest(t *testing.T) {
	names := []string{"search", "info", "download", "history", "cache", "config", "help", "last"}
	tests := []struct {
		word, want string
		ok         bool
	}{
		{"serch", "search", true},
		{"dowload", "download", true},
		{"hlep", "help", true},
		{"inf", "info", true},
		{"xyzzy", "", false},
		{"searchhhhh", "", false},
	}
	for _, tc := range tests {
		got, ok := closest(tc.word, names)
		if got != tc.want || ok != tc.ok {
			t.Errorf("closest(%q) = %q, %v; want %q, %v", tc.word, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPromptText(t *testing.T) {
	page := func(platforms []string, n int) app.SearchPage {
		p := app.SearchPage{Query: app.SearchQuery{Platforms: platforms}}
		for i := 0; i < n; i++ {
			p.Results = append(p.Results, app.IndexedEntry{Index: i + 1})
		}
		return p
	}
	tests := []struct {
		name string
		page app.SearchPage
		have bool
		want string
	}{
		{"no search yet", app.SearchPage{}, false, "mothdex> "},
		{"a platform filter", page([]string{"snes"}, 2), true, "mothdex[snes:2]> "},
		{"no filter says all", page(nil, 10), true, "mothdex[all:10]> "},
		{"several platforms", page([]string{"snes", "nes"}, 3), true, "mothdex[snes,nes:3]> "},
		{"control characters never reach the prompt", page([]string{"sn\x1b[2Jes"}, 1), true, "mothdex[sn[2Jes:1]> "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := promptText(tc.page, tc.have)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if strings.ContainsFunc(got, unicode.IsControl) {
				t.Fatalf("%q has a control character: the line editor would refuse it", got)
			}
		})
	}
}

func testCommands() []cli.CommandInfo {
	global := []cli.FlagInfo{{Name: "json"}, {Name: "quiet", Short: "q"}}
	with := func(fs ...cli.FlagInfo) []cli.FlagInfo { return append(append([]cli.FlagInfo{}, fs...), global...) }
	return []cli.CommandInfo{
		{Name: "search", Flags: with(cli.FlagInfo{Name: "platform", Short: "p", TakesValue: true}, cli.FlagInfo{Name: "region", Short: "r", TakesValue: true},
			cli.FlagInfo{Name: "sort", TakesValue: true}, cli.FlagInfo{Name: "export", Short: "e", TakesValue: true}, cli.FlagInfo{Name: "format", TakesValue: true})},
		{Name: "info", Flags: with(cli.FlagInfo{Name: "prefer", TakesValue: true})},
		{Name: "download", Flags: with(cli.FlagInfo{Name: "prefer", TakesValue: true}, cli.FlagInfo{Name: "link", TakesValue: true}, cli.FlagInfo{Name: "output", Short: "o", TakesValue: true})},
		{Name: "boxart", Flags: global},
		{Name: "config", Subs: []cli.CommandInfo{{Name: "get"}, {Name: "list"}, {Name: "reset"}, {Name: "set"}}},
		{Name: "cache", Subs: []cli.CommandInfo{{Name: "clear"}, {Name: "stats"}}},
	}
}

func TestComplete(t *testing.T) {
	src := completionSources{
		commands:   testCommands(),
		platforms:  func() []string { return []string{"snes", "nes", "gba"} },
		regions:    func() []string { return []string{"us", "eu", "jp"} },
		configKeys: func() []string { return []string{"download.on_exists", "interface.color", "interface.page_size"} },
		cacheNS:    func() []string { return []string{"search", "entry", "covers"} },
		entryLinks: func() ([]string, []string, []string) {
			return []string{"En", "Pt", "En"}, []string{"gbc"}, []string{"Myrient"}
		},
		pageLen: func() int { return 3 },
		paths:   func(string) []string { return []string{"out.csv", "out.json"} },
	}
	tests := []struct {
		name, line string
		want       []string
	}{
		{"command names include the shell's own", "se", []string{"search"}},
		{"all commands on an empty line", "", []string{"boxart", "cache", "clear", "config", "download", "exit", "help", "info", "last", "n", "p", "page", "quit", "search", "sort"}},
		{"case-insensitive prefix", "SE", []string{"search"}},
		{"flag names, long and short", "search -", []string{"--export", "--format", "--json", "--platform", "--quiet", "--region", "--sort", "-e", "-p", "-q", "-r"}},
		{"a long flag prefix", "download --pr", []string{"--prefer"}},
		{"platform ids after -p", "search zelda -p ", []string{"gba", "nes", "snes"}},
		{"platform prefix", "search -p sn", []string{"snes"}},
		{"region ids after --region", "search --region e", []string{"eu"}},
		{"sort keys", "search --sort ", []string{"relevance", "size", "title"}},
		{"prefer keys", "download 1 --prefer ", []string{"format=", "lang=", "size=", "source="}},
		{"prefer size values", "download 1 --prefer size=s", []string{"size=smallest"}},
		{"prefer lang values come from the last entry", "download 1 --prefer lang=", []string{"lang=En", "lang=Pt"}},
		{"prefer source values", "info 1 --prefer source=M", []string{"source=Myrient"}},
		{"entry numbers of the last page", "info ", []string{"1", "2", "3"}},
		{"download takes numbers too", "d ", []string{"1", "2", "3"}},
		{"config subcommands", "config ", []string{"get", "list", "reset", "set"}},
		{"config keys", "config get ", []string{"download.on_exists", "interface.color", "interface.page_size"}},
		{"config reset also offers all", "config reset a", []string{"all"}},
		{"config set enum values", "config set interface.color ", []string{"always", "auto", "never"}},
		{"config set offers nothing for a free value", "config set interface.page_size ", nil},
		{"config set on_exists does not offer overwrite", "config set download.on_exists ", []string{"rename", "skip"}},
		{"cache subcommands", "cache c", []string{"clear"}},
		{"cache namespaces", "cache clear ", []string{"covers", "entry", "search"}},
		{"export paths", "search mario -e o", []string{"out.csv", "out.json"}},
		{"export formats", "search mario --format j", []string{"json"}},
		{"help takes command names", "help se", []string{"search"}},
		{"sort takes keys", "sort t", []string{"title"}},
		{"a bool flag does not eat the next word", "info -q ", []string{"1", "2", "3"}},
		{"unknown command", "zzz ", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got, _ := complete(tc.line, len([]rune(tc.line)), src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("complete(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestCompleteSplitsTheLineAroundTheWord(t *testing.T) {
	src := completionSources{commands: testCommands(), platforms: func() []string { return []string{"snes"} }}
	head, cands, tail := complete("search -p sn zelda", len([]rune("search -p sn")), src)
	if head != "search -p " || !reflect.DeepEqual(cands, []string{"snes"}) || tail != " zelda" {
		t.Fatalf("head %q cands %q tail %q", head, cands, tail)
	}
	// the cursor is counted in runes, so a wide character before it must not shift the split
	head, _, tail = complete("search 日本 -p ", len([]rune("search 日本 -p ")), src)
	if head != "search 日本 -p " || tail != "" {
		t.Fatalf("head %q tail %q", head, tail)
	}
}

func TestCompleteOffersNothingWhenASourceFails(t *testing.T) {
	src := completionSources{commands: testCommands(), platforms: func() []string { return nil }}
	if _, got, _ := complete("search -p ", len("search -p "), src); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestEntryLinksKeepOnlyWordsThatCanBeTyped(t *testing.T) {
	sh := &Shell{o: Options{Runner: &fakeRunner{entry: &mothdex.Entry{Links: []mothdex.Link{
		{Extras: []string{"En,Pt", "Rev A"}, Format: "gbc", SourceName: "Archive org\x1b[31m"},
		{Extras: []string{"\x1b]0;x\x07"}, Format: "zip", SourceName: "Myrient"},
	}}}}}
	langs, formats, sources := sh.entryLinks()
	for _, v := range append(append(append([]string{}, langs...), formats...), sources...) {
		if strings.ContainsAny(v, "\x1b\a \t,") {
			t.Errorf("%q cannot be typed as one word, or carries a control character", v)
		}
	}
	if got := strings.Join(langs, "|"); !strings.HasPrefix(got, "En|Pt|Rev|A") {
		t.Errorf("langs %q", got)
	}
	if !reflect.DeepEqual(formats, []string{"gbc", "zip"}) || !reflect.DeepEqual(sources, []string{"Myrient"}) {
		t.Errorf("formats %q sources %q", formats, sources)
	}
}

func TestCompletionIdsFromTheServerAreCleanedBeforeTheyReachTheTerminal(t *testing.T) {
	r := newRig(t, false)
	r.sh.o.Service = hostileService{}
	src := r.sh.sources(context.Background())
	for name, got := range map[string][]string{"platforms": src.platforms(), "regions": src.regions()} {
		if want := []string{"snes", "n]0;es"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q, want %q (a space, an empty id and the control bytes must go)", name, got, want)
		}
	}
}

// hostileService answers the lists Tab reads with ids that carry terminal escapes.
type hostileService struct{ fakeService }

func (hostileService) Platforms(context.Context, app.PlatformFilter) (app.Result[[]mothdex.Platform], error) {
	return app.Result[[]mothdex.Platform]{Value: []mothdex.Platform{{ID: "snes"}, {ID: "n\x1b]0;es"}, {ID: "has space"}, {ID: ""}}}, nil
}

func (hostileService) Regions(context.Context) (app.Result[[]mothdex.Region], error) {
	return app.Result[[]mothdex.Region]{Value: []mothdex.Region{{ID: "snes"}, {ID: "n\x1b]0;es"}, {ID: "a,b"}, {ID: "\x07"}}}, nil
}

func TestCompletePathsLeaveOutNamesThatCannotBeTyped(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"out.csv", "my file.csv", "out\x1b[2J.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Skipf("the file system refuses the name %q: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "outdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := completePaths(filepath.Join(dir, "out"))
	want := []string{filepath.Join(dir, "out.csv"), filepath.Join(dir, "outdir") + string(filepath.Separator)}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
