package gui

import (
	"math"
	"strconv"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// Tier is the size class of docs/ux/gui.md section 1.1, chosen from the logical height.
type Tier int

// Tiers.
const (
	Compact  Tier = iota // logical height below 600
	Standard             // 600 to 899
	Large                // 900 and above
)

// Thresholds of section 1.1 and 1.2.
const (
	minLogicalW     = 640  // the smallest layout; a smaller display is scaled by SDL
	minLogicalH     = 480  //
	scaleFromH      = 1200 // above this many pixels tall the 1080 layout is scaled by an integer
	referenceH      = 1080
	standardFromH   = 600
	largeFromH      = 900
	narrowBelowW    = 800
	autoSafePercent = 5
	autoSafeMinW    = 1280 // a display at least this big is taken to be a TV
	autoSafeMinH    = 720
	maxSafePercent  = 10
)

// SafeArea is the gui.safe_area setting: auto, or a percent of each edge.
type SafeArea struct {
	Auto    bool
	Percent int // 0 to 10 when Auto is false
}

// AutoSafeArea is the default of the setting.
var AutoSafeArea = SafeArea{Auto: true}

// ParseSafeArea reads the value of gui.safe_area: "auto" or an integer from 0 to 10.
func ParseSafeArea(s string) (SafeArea, bool) {
	if s == "auto" {
		return AutoSafeArea, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > maxSafePercent {
		return SafeArea{}, false
	}
	return SafeArea{Percent: n}, true
}

// percentFor resolves the setting for a display: auto is 5 % on a display of at least 1280x720 and 0 % below.
func (s SafeArea) percentFor(displayW, displayH int) int {
	if !s.Auto {
		return s.Percent
	}
	if displayW >= autoSafeMinW && displayH >= autoSafeMinH {
		return autoSafePercent
	}
	return 0
}

// tierValues holds a value for each tier, in the order of the tables of sections 1.3 and 1.4.
type tierValues [3]int

func (v tierValues) at(t Tier) int { return v[t] }

// The tables of docs/ux/gui.md sections 1.3 and 1.4.
var (
	fontTitle     = tierValues{26, 34, 50}
	fontDisplay   = tierValues{35, 46, 68} // Handjet, title x 1.35 (DESIGN.md Type)
	fontBody      = tierValues{20, 26, 38}
	fontSecondary = tierValues{17, 21, 31}
	fontMinimum   = tierValues{16, 18, 27}
	spaceUnit     = tierValues{8, 10, 16}
	topBarHeight  = tierValues{36, 48, 68}
	hintBarHeight = tierValues{36, 48, 68}
	rowOneLine    = tierValues{40, 52, 76}
	rowTwoLines   = tierValues{52, 72, 104}
	tileWidth     = tierValues{180, 280, 420}
	tileHeight    = tierValues{96, 150, 220}
	keyMin        = tierValues{48, 60, 88}
	focusRing     = tierValues{3, 4, 6}
	progressBar   = tierValues{8, 10, 14}
	pixelCell     = tierValues{2, 3, 4} // P, DESIGN.md Pixel grid
)

// Metrics is everything layout needs to know about the display and the settings. It is a value: a resize or
// a setting change builds a new one.
type Metrics struct {
	DisplayW, DisplayH int
	Scale              int  // integer render scale (SDL_RenderSetScale)
	W, H               int  // logical size: the layout's coordinate space
	FitLogical         bool // the display is smaller than 640x480: SDL_RenderSetLogicalSize(W, H) letterboxes
	Tier               Tier
	Narrow             bool
	LargeText          bool
	Safe               draw.Rect // the safe rectangle, in logical pixels

	Unit, TopBar, HintBar, Row1, Row2 int
	TileW, TileH, Key, Ring, Progress int
	Pixel                             int // P, the pixel cell every shape snaps to: 2, 3 or 4
	Title, Body, Secondary, Minimum   int // type sizes in pixels
	Display                           int // the size of the display face
}

// NewMetrics computes the layout metrics for a display of w x h pixels.
func NewMetrics(w, h int, safe SafeArea, largeText bool) Metrics {
	if w <= 0 || h <= 0 {
		w, h = minLogicalW, minLogicalH
	}
	m := Metrics{DisplayW: w, DisplayH: h, Scale: 1, W: w, H: h, LargeText: largeText}
	switch {
	case w < minLogicalW || h < minLogicalH:
		m.W, m.H, m.FitLogical = minLogicalW, minLogicalH, true
	case h > scaleFromH:
		m.Scale = h / referenceH
		m.W, m.H = w/m.Scale, h/m.Scale
	}
	switch {
	case m.H < standardFromH:
		m.Tier = Compact
	case m.H < largeFromH:
		m.Tier = Standard
	default:
		m.Tier = Large
	}
	m.Narrow = m.W < narrowBelowW

	t := m.Tier
	m.Pixel = pixelCell.at(t)
	m.Unit = spaceUnit.at(t)
	m.Ring, m.Progress = snapToCell(focusRing.at(t), m.Pixel), snapToCell(progressBar.at(t), m.Pixel)
	m.TileW, m.TileH = tileWidth.at(t), tileHeight.at(t)
	grow := func(n int) int { return n }
	if largeText {
		grow = largerText
	}
	m.Title, m.Body, m.Display = grow(fontTitle.at(t)), grow(fontBody.at(t)), grow(fontDisplay.at(t))
	m.Secondary, m.Minimum = grow(fontSecondary.at(t)), grow(fontMinimum.at(t))
	m.TopBar, m.HintBar = grow(topBarHeight.at(t)), grow(hintBarHeight.at(t))
	m.Row1, m.Row2, m.Key = grow(rowOneLine.at(t)), grow(rowTwoLines.at(t)), grow(keyMin.at(t))

	pct := safe.percentFor(w, h)
	insetX, insetY := m.W*pct/100, m.H*pct/100
	m.Safe = draw.Rect{X: insetX, Y: insetY, W: m.W - 2*insetX, H: m.H - 2*insetY}
	return m
}

// snapToCell rounds n to a multiple of the pixel cell, at least one cell: ring = P x round(ring / P).
func snapToCell(n, cell int) int { return max(cell, int(math.Round(float64(n)/float64(cell)))*cell) }

// largerText is the 1.25 multiplier of gui.text_size = large, rounded to an even number of pixels.
func largerText(n int) int { return int(math.Round(float64(n)*1.25/2)) * 2 }

// TopBarRect is the top bar: the full width of the safe rectangle.
func (m Metrics) TopBarRect() draw.Rect {
	return draw.Rect{X: m.Safe.X, Y: m.Safe.Y, W: m.Safe.W, H: m.TopBar}
}

// HintBarRect is the hint bar at the bottom of the safe rectangle.
func (m Metrics) HintBarRect() draw.Rect {
	return draw.Rect{X: m.Safe.X, Y: m.Safe.Bottom() - m.HintBar, W: m.Safe.W, H: m.HintBar}
}

// ContentRect is the area between the bars, with the 2u padding of section 1.4.
func (m Metrics) ContentRect() draw.Rect {
	top, bottom := m.TopBarRect().Bottom(), m.HintBarRect().Y
	return draw.Rect{X: m.Safe.X, Y: top, W: m.Safe.W, H: bottom - top}.Inset(2 * m.Unit)
}

// LineHeight is 1.25 times the size, the line height of section 1.3.
func LineHeight(size int) int { return size * 5 / 4 }
