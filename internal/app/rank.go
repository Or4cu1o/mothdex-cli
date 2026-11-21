package app

import (
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// rankLinks is the pure ranking rule of app-service.md section 3 (T-16 case 28). Links whose URL
// fails the downloader URL policy are removed from the candidates and returned after the ranked
// ones as unavailable. Among the rest, "Game" links are kept if any exist; then the order is
// lexicographic by: first preferred language found in extras, format position, source position,
// size (smallest or largest), API order.
func rankLinks(links []mothdex.Link, pref Preference, allowed []string) []LinkChoice {
	var cands, unavailable []LinkChoice
	for i, l := range links {
		c := LinkChoice{Index: i + 1, Link: l, Available: true}
		if _, err := proxy.CheckURL(l.URL, allowed); err != nil {
			c.Available = false
			c.Reasons = []string{"host not allowed: " + hostOf(l.URL)}
			unavailable = append(unavailable, c)
			continue
		}
		cands = append(cands, c)
	}
	if slices.ContainsFunc(cands, func(c LinkChoice) bool { return c.Link.Type == "Game" }) {
		cands = slices.DeleteFunc(cands, func(c LinkChoice) bool { return c.Link.Type != "Game" })
	}
	keys := make([][3]int, len(cands))
	for i := range cands {
		keys[i] = [3]int{langRank(cands[i].Link.Extras, pref.Languages), indexOfFold(pref.Formats, cands[i].Link.Format), indexOfFold(pref.Sources, cands[i].Link.SourceName)}
		cands[i].Reasons = reasons(cands[i].Link, pref, keys[i])
	}
	order := make([]int, len(cands))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ka, kb := keys[order[a]], keys[order[b]]
		for j := range ka {
			if ka[j] != kb[j] {
				return ka[j] < kb[j]
			}
		}
		sa, sb := cands[order[a]].Link.Size, cands[order[b]].Link.Size
		switch pref.Size {
		case SizeSmallest:
			return sa < sb
		case SizeLargest:
			return sa > sb
		}
		return false
	})
	out := make([]LinkChoice, 0, len(links))
	for rank, i := range order {
		c := cands[i]
		c.Score = len(cands) - rank
		out = append(out, c)
	}
	return append(out, unavailable...)
}

const noMatch = 1 << 20 // sorts after every real position

func langRank(extras, prefs []string) int {
	best := noMatch
	for _, e := range extras {
		for _, tok := range strings.FieldsFunc(e, func(r rune) bool { return r == ',' || r == ' ' }) {
			if i := indexOfFold(prefs, tok); i < best {
				best = i
			}
		}
	}
	return best
}

func indexOfFold(list []string, v string) int {
	for i, p := range list {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(v)) && v != "" {
			return i
		}
	}
	return noMatch
}

func reasons(l mothdex.Link, pref Preference, k [3]int) []string {
	var out []string
	if k[0] != noMatch {
		out = append(out, "lang="+SanitizeText(strings.Join(l.Extras, ",")))
	}
	if k[1] != noMatch {
		out = append(out, "format="+SanitizeText(l.Format))
	}
	if k[2] != noMatch {
		out = append(out, "source="+SanitizeText(l.SourceName))
	}
	if pref.Size != SizeNone {
		out = append(out, "size="+string(pref.Size))
	}
	return out
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "invalid URL"
	}
	return SanitizeText(u.Hostname())
}
