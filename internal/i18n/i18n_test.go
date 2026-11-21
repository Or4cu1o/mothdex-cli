package i18n

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Or4cu1o/mothdex-cli/locales"
)

var bidi = string(rune(0x202E)) // right-to-left override

func embedded(t *testing.T) *Bundle {
	t.Helper()
	b, err := Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	if w := b.Warnings(); len(w) != 0 {
		t.Fatalf("embedded locales skipped: %v", w)
	}
	return b
}

// errorCodes are the codes of cli-output.md section 4; each needs errors.<code>.message and .hint.
var errorCodes = []string{"internal", "usage", "invalid_config", "invalid_request", "confirmation_required", "unmapped_platform",
	"not_found", "no_session", "index_out_of_range", "network", "rate_limited", "upstream", "api_incompatible", "untrusted_url",
	"untrusted_redirect", "size_mismatch", "integrity", "permission_denied", "disk_full", "exists", "path_rejected", "unsafe_name", "canceled"}

// The CI check: translations are complete where promised, never define keys en_us lacks, and
// keep the placeholders of the source string.
func TestLocalesAgainstEnUS(t *testing.T) {
	b := embedded(t)
	for _, code := range errorCodes {
		for _, part := range []string{"message", "hint"} {
			if _, ok := b.Catalog(Fallback)["errors."+code+"."+part]; !ok {
				t.Errorf("en_us lacks errors.%s.%s", code, part)
			}
		}
	}
	complete := map[string]bool{"pt_br": true, "es_es": true, "ru_ru": true}
	for _, id := range b.Locales() {
		if extra := b.Extra(id); len(extra) != 0 {
			t.Errorf("%s defines keys en_us does not: %v", id, extra)
		}
		if missing := b.Missing(id); complete[id] && len(missing) != 0 {
			t.Errorf("%s is missing %d keys: %v", id, len(missing), missing)
		} else if !complete[id] && len(missing) != 0 {
			t.Logf("%s falls back to en_us for %d keys (best effort locale)", id, len(missing))
		}
		for k, v := range b.Catalog(id) {
			src, ok := b.Catalog(Fallback)[k]
			if pluralKey(k) && !strings.Contains(v, "{count}") {
				t.Errorf("%s %s: a plural form must carry {count}", id, k)
			}
			if ok && !samePlaceholders(src, v) && !pluralKey(k) {
				t.Errorf("%s %s: placeholders %v differ from en_us %v", id, k, placeholders(v), placeholders(src))
			}
		}
	}
	if len(b.Locales()) != 7 {
		t.Fatalf("locales %v", b.Locales())
	}
}

var placeholderRE = regexp.MustCompile(`\{[a-z0-9_]+\}`)

func placeholders(s string) []string {
	p := placeholderRE.FindAllString(s, -1)
	slices.Sort(p)
	return slices.Compact(p)
}

func samePlaceholders(a, b string) bool { return slices.Equal(placeholders(a), placeholders(b)) }

// Plural forms differ per language (ru has few and many), so they are not compared one to one.
func pluralKey(k string) bool {
	_, last, ok := cutLast(k)
	return ok && isPluralForm(last)
}

func TestFallbackPerKeyThenKey(t *testing.T) {
	en := fstest.MapFS{"en_us.yaml": {Data: []byte("a: 'A en'\nb: 'B en {x}'\nc: 'C en'\n")}}
	pt := fstest.MapFS{"pt_br.yaml": {Data: []byte("locale:\n  name: pt\na: 'A pt'\nb: 'B pt {x}'\n")}}
	b, err := Load(en, pt)
	if err != nil {
		t.Fatal(err)
	}
	tr := b.New("pt_br")
	if tr.T("a", nil) != "A pt" || tr.T("b", Args{"x": 1}) != "B pt 1" {
		t.Fatalf("own keys: %q %q", tr.T("a", nil), tr.T("b", Args{"x": 1}))
	}
	if got := tr.T("c", nil); got != "C en" {
		t.Fatalf("missing key must fall back to en_us, got %q", got)
	}
	if got := tr.T("nope", nil); got != "nope" {
		t.Fatalf("unknown key must return the key, got %q", got)
	}
	if got := tr.T("b", nil); got != "B pt {x}" {
		t.Fatalf("missing argument stays visible: %q", got)
	}
	if b.New("xx_yy").Locale() != Fallback {
		t.Fatal("an unknown locale behaves as en_us")
	}
	if got := b.Missing("pt_br"); len(got) != 1 || got[0] != "c" {
		t.Fatalf("Missing: %v", got)
	}
}

func TestExternalFolderOverridesPerKeyAndAddsLocales(t *testing.T) {
	emb := fstest.MapFS{"en_us.yaml": {Data: []byte("a: 'A'\nb: 'B'\n")}, "pt_br.yaml": {Data: []byte("a: 'A pt'\nb: 'B pt'\n")}}
	ext := fstest.MapFS{
		"pt_br.yaml": {Data: []byte("b: 'B custom'\n")},
		"de_de.yaml": {Data: []byte("a: 'A de'\n")},
		"readme.txt": {Data: []byte("ignored")},
	}
	b, err := Load(emb, ext)
	if err != nil {
		t.Fatal(err)
	}
	pt := b.New("pt_br")
	if pt.T("a", nil) != "A pt" || pt.T("b", nil) != "B custom" {
		t.Fatalf("override is per key: %q %q", pt.T("a", nil), pt.T("b", nil))
	}
	if de := b.New("de_de"); de.T("a", nil) != "A de" || de.T("b", nil) != "B" {
		t.Fatal("an external locale is added and falls back")
	}
}

func TestHostileLocaleFilesAreSkippedOrNeutralised(t *testing.T) {
	en := fstest.MapFS{"en_us.yaml": {Data: []byte("a: 'A'\n")}}
	bad := fstest.MapFS{
		"fr_fr.yaml": {Data: []byte("a: [1, 2]\n")},                        // not text
		"it_it.yaml": {Data: []byte("'../x': 'v'\n")},                      // key outside the key pattern
		"nl_nl.yaml": {Data: []byte(`a: "x\e[31mred` + bidi + `"` + "\n")}, // terminal escape, bidi override
		"ja_jp.yaml": {Data: []byte(": : :\n\t-")},                         // not YAML
	}
	b, err := Load(en, bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Warnings()) != 3 {
		t.Fatalf("warnings: %v", b.Warnings())
	}
	if got := b.New("nl_nl").T("a", nil); got != "x[31mred" {
		t.Fatalf("controls must be stripped: %q", got)
	}
	if _, err := Load(fstest.MapFS{"pt_br.yaml": {Data: []byte("a: b\n")}}); err == nil {
		t.Fatal("en_us is required")
	}
}

func TestArgumentsAreSanitisedAndNotReExpanded(t *testing.T) {
	b, _ := Load(fstest.MapFS{"en_us.yaml": {Data: []byte("m: 'file {name} of {other}'\n")}})
	got := b.New("en_us").T("m", Args{"name": "\x1b[2J{other}" + bidi, "other": "O"})
	if got != "file [2J{other} of O" {
		t.Fatalf("%q", got)
	}
}

func TestPlurals(t *testing.T) {
	b := embedded(t)
	tests := []struct {
		locale string
		n      int
		want   string
	}{
		{"en_us", 1, "Found 1 result"}, {"en_us", 0, "Found 0 results"}, {"en_us", 5, "Found 5 results"},
		{"pt_br", 1, "1 resultado encontrado"}, {"pt_br", 2, "2 resultados encontrados"},
		{"ru_ru", 1, "Найден 1 результат"}, {"ru_ru", 3, "Найдено 3 результата"}, {"ru_ru", 5, "Найдено 5 результатов"}, {"ru_ru", 21, "Найден 21 результат"},
		{"zh_cn", 1, "找到 1 个结果"}, // no singular form: other is the fallback
	}
	for _, tt := range tests {
		if got := b.New(tt.locale).N("ui.search.found", tt.n, nil); got != tt.want {
			t.Errorf("%s %d: %q, want %q", tt.locale, tt.n, got, tt.want)
		}
	}
	if got := b.New("ja_jp").N("ui.search.found", 2, nil); got != "2 件の結果が見つかりました" {
		t.Fatal(got)
	}
	if b.New("en_us").N("no.such.key", 1, nil) != "no.such.key" {
		t.Fatal("unknown plural key returns the key")
	}
}

func TestNormalizeAndDetect(t *testing.T) {
	for in, want := range map[string]string{"pt_BR.UTF-8": "pt_br", "pt-BR": "pt_br", "en_US@euro": "en_us", "ru": "ru", "C": "", "POSIX": "", "": "",
		"zh_Hans_CN": "zh_cn", "C.UTF-8": "", "x": "", "toolonglang_US": ""} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name string
		env  map[string]string
		sys  []string
		want string
	}{
		{"LC_ALL wins", map[string]string{"LC_ALL": "es_ES.UTF-8", "LC_MESSAGES": "ru_RU", "LANG": "pt_BR"}, nil, "es_es"},
		{"LC_MESSAGES over LANG", map[string]string{"LC_MESSAGES": "ru_RU.UTF-8", "LANG": "pt_BR"}, nil, "ru_ru"},
		{"LANG", map[string]string{"LANG": "pt_BR.UTF-8"}, nil, "pt_br"},
		{"C locale is skipped", map[string]string{"LC_ALL": "C", "LANG": "pt_BR"}, nil, "pt_br"},
		{"Windows UI language", nil, []string{"ja-JP", "en-US"}, "ja_jp"},
		{"environment before Windows", map[string]string{"LANG": "ru_RU"}, []string{"ja-JP"}, "ru_ru"},
		{"nothing", nil, nil, ""},
	}
	for _, tt := range tests {
		sys := func() []string { return tt.sys }
		if got := Detect(env(tt.env), sys); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	b := embedded(t)
	none := func(string) string { return "" }
	lang := func(v string) func(string) string {
		return func(k string) string { return map[string]string{"LANG": v}[k] }
	}
	tests := []struct {
		setting string
		env     func(string) string
		want    string
	}{
		{"auto", lang("pt_BR.UTF-8"), "pt_br"},
		{"", lang("es_ES.UTF-8"), "es_es"},
		{"AUTO", lang("ru_RU"), "ru_ru"},
		{"auto", lang("pt_PT.UTF-8"), "pt_br"}, // same language, other region
		{"auto", lang("es_MX"), "es_es"},
		{"auto", lang("zh_TW"), "zh_cn"},
		{"auto", lang("de_DE"), "en_us"}, // not available
		{"auto", none, "en_us"},
		{"auto", lang("C"), "en_us"},
		{"PT_BR", none, "pt_br"}, // explicit, case-insensitive
		{"ru-RU", none, "ru_ru"},
		{"fr_FR", lang("pt_BR"), "en_us"}, // an explicit unknown language does not fall to the environment
		{"pt", none, "pt_br"},
	}
	for _, tt := range tests {
		if got := b.Resolve(tt.setting, tt.env, nil); got != tt.want {
			t.Errorf("Resolve(%q): %q, want %q", tt.setting, got, tt.want)
		}
	}
}

func TestEmbeddedLocalesTranslateTheErrorTable(t *testing.T) {
	b := embedded(t)
	got := b.New("pt_br").T("errors.index_out_of_range.message", Args{"index": 9, "count": 3})
	if got != "O resultado nº 9 não existe (a última busca tem 3)." {
		t.Fatal(got)
	}
	if b.New("zh_cn").T("errors.disk_full.message", Args{"path": "/x", "needed": "1 GB"}) != "Not enough free space in /x (needs 1 GB)." {
		t.Fatal("best-effort locales fall back to en_us per key")
	}
	if b.New("zh_cn").T("ui.messages.yes", nil) != "是" {
		t.Fatal("translated keys of a best-effort locale are used")
	}
}
