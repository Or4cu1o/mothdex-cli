package tui

// Terminal size limits of tui.md section 9.
const (
	minCols    = 60 // below: too small
	minRows    = 16
	fullCols   = 80 // below: drop columns
	shortRows  = 20 // below: hide the secondary parts
	statusFull = 8  // keys in the status bar at full width
	statusTiny = 4  // keys at 60-79 columns, plus "? more"
)

// sizeTier is how much of the reference layout fits.
type sizeTier int

const (
	tierFull sizeTier = iota
	tierNarrow
	tierShort
	tierTooSmall
)

// tier classifies a terminal size. Too small wins; then few rows (the Search screen hides Recent,
// the Entry screen hides Reasons); then few columns (columns drop, the status bar shortens).
func tier(w, h int) sizeTier {
	switch {
	case w < minCols || h < minRows:
		return tierTooSmall
	case h < shortRows:
		return tierShort
	case w < fullCols:
		return tierNarrow
	}
	return tierFull
}

// col names a column of a results list.
type col int

const (
	colMark col = iota
	colIndex
	colTitle
	colPlatform
	colRegions
	colLinks
	colSize
)

// columns says which columns of the results list are shown at a width, in the CLI's drop order
// (cli-conventions section 3): below 80 Links, Regions and Size go; the title and platform stay.
func columns(t sizeTier, w int) []col {
	if t == tierTooSmall {
		return nil
	}
	cols := []col{colMark, colIndex, colTitle, colPlatform}
	if w >= fullCols {
		cols = append(cols, colRegions, colLinks, colSize)
	}
	return cols
}

// statusKeyLimit is how many keys the status bar shows.
func statusKeyLimit(w int) int {
	if w < fullCols {
		return statusTiny
	}
	return statusFull
}
