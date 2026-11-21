package shell

import "slices"

// maxEditDistance is how far a mistyped command may be from a real one to be suggested.
const maxEditDistance = 2

// closest returns the name nearest to word within maxEditDistance (ties go to the first in sorted
// order), or false when none is close enough.
func closest(word string, names []string) (string, bool) {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	best, bestDist := "", maxEditDistance+1
	for _, n := range sorted {
		if d := editDistance(word, n); d < bestDist {
			best, bestDist = n, d
		}
	}
	return best, bestDist <= maxEditDistance
}

// editDistance is the Levenshtein distance between two strings, counted in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
