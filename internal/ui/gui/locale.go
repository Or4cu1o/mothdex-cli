package gui

// englishOnly are the interface languages whose scripts the embedded font does not have (CJK, Devanagari):
// the GUI shows en_us strings for them and says so once on Home (docs/ux/gui.md section 1.3).
var englishOnly = map[string]bool{"zh_cn": true, "ja_jp": true, "hi_in": true}

// NeedsEnglish reports whether the GUI cannot draw the language and must show en_us instead.
func NeedsEnglish(locale string) bool { return englishOnly[locale] }
