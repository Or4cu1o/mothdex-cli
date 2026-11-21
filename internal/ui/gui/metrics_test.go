package gui

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// Tier, scale, logical size and width class for the displays of docs/ux/gui.md section 1.1.
func TestTierScaleAndWidthClass(t *testing.T) {
	tests := []struct {
		name       string
		w, h       int
		tier       Tier
		scale      int
		logW, logH int
		fit        bool
		narrow     bool
	}{
		{"480x320 handheld: drawn as 640x480 and scaled by SDL", 480, 320, Compact, 1, 640, 480, true, true},
		{"640x480", 640, 480, Compact, 1, 640, 480, false, true},
		{"854x480", 854, 480, Compact, 1, 854, 480, false, false},
		{"720x576 PAL", 720, 576, Compact, 1, 720, 576, false, true},
		{"720x720 square handheld", 720, 720, Standard, 1, 720, 720, false, true},
		{"1024x768", 1024, 768, Standard, 1, 1024, 768, false, false},
		{"1280x720 reference", 1280, 720, Standard, 1, 1280, 720, false, false},
		{"1280x800", 1280, 800, Standard, 1, 1280, 800, false, false},
		{"1920x1080", 1920, 1080, Large, 1, 1920, 1080, false, false},
		{"1920x1200 is not above 1200", 1920, 1200, Large, 1, 1920, 1200, false, false},
		{"2560x1440 floor(1440/1080) is 1", 2560, 1440, Large, 1, 2560, 1440, false, false},
		{"3840x2160 4K: the 1080 layout at scale 2", 3840, 2160, Large, 2, 1920, 1080, false, false},
		{"599 is compact", 1000, 599, Compact, 1, 1000, 599, false, false},
		{"600 is standard", 1000, 600, Standard, 1, 1000, 600, false, false},
		{"899 is standard", 1000, 899, Standard, 1, 1000, 899, false, false},
		{"900 is large", 1000, 900, Large, 1, 1000, 900, false, false},
		{"799 wide is narrow", 799, 600, Standard, 1, 799, 600, false, true},
		{"800 wide is not", 800, 600, Standard, 1, 800, 600, false, false},
		{"no size yet", 0, 0, Compact, 1, 640, 480, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetrics(tc.w, tc.h, AutoSafeArea, false)
			if m.Tier != tc.tier || m.Scale != tc.scale || m.W != tc.logW || m.H != tc.logH || m.FitLogical != tc.fit || m.Narrow != tc.narrow {
				t.Fatalf("tier %d scale %d logical %dx%d fit %v narrow %v; want %d %d %dx%d %v %v",
					m.Tier, m.Scale, m.W, m.H, m.FitLogical, m.Narrow, tc.tier, tc.scale, tc.logW, tc.logH, tc.fit, tc.narrow)
			}
		})
	}
}

// The values of the tables of sections 1.3 and 1.4 for each tier.
func TestSizesFollowTheTablesOfTheSpec(t *testing.T) {
	tests := []struct {
		w, h                                int
		title, body, secondary, minimum     int
		unit, bar, row1, row2, tileW, tileH int
		key, ring, progress, radius         int
	}{
		{640, 480, 26, 20, 17, 16, 8, 36, 40, 52, 180, 96, 48, 3, 8, 6},
		{1280, 720, 34, 26, 21, 18, 10, 48, 52, 72, 280, 150, 60, 4, 10, 8},
		{1920, 1080, 50, 38, 31, 27, 16, 68, 76, 104, 420, 220, 88, 6, 14, 12},
	}
	for _, tc := range tests {
		m := NewMetrics(tc.w, tc.h, SafeArea{}, false)
		got := []int{m.Title, m.Body, m.Secondary, m.Minimum, m.Unit, m.TopBar, m.HintBar, m.Row1, m.Row2, m.TileW, m.TileH, m.Key, m.Ring, m.Progress, m.Radius}
		want := []int{tc.title, tc.body, tc.secondary, tc.minimum, tc.unit, tc.bar, tc.bar, tc.row1, tc.row2, tc.tileW, tc.tileH, tc.key, tc.ring, tc.progress, tc.radius}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%dx%d: value %d is %d, want %d (%v)", tc.w, tc.h, i, got[i], want[i], got)
				break
			}
		}
	}
}

// gui.text_size = large multiplies sizes and row heights by 1.25, rounded to an even pixel.
func TestLargeTextMultipliesByOneAndAQuarterRoundedToEven(t *testing.T) {
	normal := NewMetrics(640, 480, SafeArea{}, false)
	large := NewMetrics(640, 480, SafeArea{}, true)
	pairs := []struct {
		name          string
		normal, large int
	}{
		{"title", normal.Title, large.Title}, {"body", normal.Body, large.Body},
		{"secondary", normal.Secondary, large.Secondary}, {"minimum", normal.Minimum, large.Minimum},
		{"row1", normal.Row1, large.Row1}, {"row2", normal.Row2, large.Row2},
		{"top bar", normal.TopBar, large.TopBar}, {"key", normal.Key, large.Key},
	}
	for _, p := range pairs {
		exact := float64(p.normal) * 1.25
		if p.large%2 != 0 || float64(p.large) < exact-1.01 || float64(p.large) > exact+1.01 {
			t.Errorf("%s: %d became %d, want about %.2f and even", p.name, p.normal, p.large, exact)
		}
	}
	if large.Body != 26 || large.Row1 != 50 {
		t.Errorf("body %d and row %d at compact large text, want 26 and 50", large.Body, large.Row1)
	}
	if large.Unit != normal.Unit || large.TileW != normal.TileW {
		t.Error("spacing and tiles do not change with the text size")
	}
}

func TestSafeRectangleForAutoAndFixedMargins(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		safe SafeArea
		want draw.Rect
	}{
		{"auto on a handheld panel is 0", 640, 480, AutoSafeArea, draw.Rect{W: 640, H: 480}},
		{"auto just below 1280x720 is 0", 1279, 720, AutoSafeArea, draw.Rect{W: 1279, H: 720}},
		{"auto below the height is 0", 1280, 719, AutoSafeArea, draw.Rect{W: 1280, H: 719}},
		{"auto at 1280x720 is 5 percent", 1280, 720, AutoSafeArea, draw.Rect{X: 64, Y: 36, W: 1152, H: 648}},
		{"auto at 1080p", 1920, 1080, AutoSafeArea, draw.Rect{X: 96, Y: 54, W: 1728, H: 972}},
		{"auto at 4K uses the real display and the logical layout", 3840, 2160, AutoSafeArea, draw.Rect{X: 96, Y: 54, W: 1728, H: 972}},
		{"zero at 1080p", 1920, 1080, SafeArea{Percent: 0}, draw.Rect{W: 1920, H: 1080}},
		{"ten percent at 1080p", 1920, 1080, SafeArea{Percent: 10}, draw.Rect{X: 192, Y: 108, W: 1536, H: 864}},
		{"three percent at 640x480", 640, 480, SafeArea{Percent: 3}, draw.Rect{X: 19, Y: 14, W: 602, H: 452}},
		{"a scaled handheld: fixed 5 on the 640x480 layout", 480, 320, SafeArea{Percent: 5}, draw.Rect{X: 32, Y: 24, W: 576, H: 432}},
	}
	for _, tc := range tests {
		if got := NewMetrics(tc.w, tc.h, tc.safe, false).Safe; got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseSafeArea(t *testing.T) {
	ok := map[string]SafeArea{"auto": AutoSafeArea, "0": {Percent: 0}, "5": {Percent: 5}, "10": {Percent: 10}, "07": {Percent: 7}}
	for in, want := range ok {
		if got, valid := ParseSafeArea(in); !valid || got != want {
			t.Errorf("%q: %+v %v", in, got, valid)
		}
	}
	for _, in := range []string{"", "11", "-1", "x", "5.5", "Auto", " 5", "5%"} {
		if _, valid := ParseSafeArea(in); valid {
			t.Errorf("%q accepted", in)
		}
	}
}

// Whatever the display, margin and text size, the frame stays inside the safe rectangle and leaves room
// for content (section 11: nothing is drawn outside the safe rectangle).
func TestBarsAndContentStayInsideTheSafeRectangle(t *testing.T) {
	sizes := [][2]int{{480, 320}, {640, 480}, {854, 480}, {720, 576}, {1280, 720}, {1920, 1080}, {3840, 2160}}
	for _, s := range sizes {
		for _, safe := range []SafeArea{AutoSafeArea, {Percent: 0}, {Percent: 5}, {Percent: 10}} {
			for _, large := range []bool{false, true} {
				m := NewMetrics(s[0], s[1], safe, large)
				full := draw.Rect{W: m.W, H: m.H}
				content := m.ContentRect()
				for name, r := range map[string]draw.Rect{"safe": m.Safe, "top": m.TopBarRect(), "hint": m.HintBarRect(), "content": content} {
					if !r.Within(m.Safe) || !r.Within(full) {
						t.Errorf("%dx%d safe=%+v large=%v: %s %+v is outside the safe rectangle %+v", s[0], s[1], safe, large, name, r, m.Safe)
					}
				}
				if content.H < 3*m.Row1 {
					t.Errorf("%dx%d safe=%+v large=%v: content is only %d px tall", s[0], s[1], safe, large, content.H)
				}
				if m.TopBarRect().Bottom() > content.Y || content.Bottom() > m.HintBarRect().Y {
					t.Errorf("%dx%d: bars overlap the content", s[0], s[1])
				}
			}
		}
	}
}

func TestLineHeightIsOneAndAQuarterTimesTheSize(t *testing.T) {
	for size, want := range map[int]int{16: 20, 20: 25, 26: 32, 50: 62} {
		if got := LineHeight(size); got != want {
			t.Errorf("LineHeight(%d) = %d, want %d", size, got, want)
		}
	}
}

func TestLanguagesTheFontCannotDrawFallBackToEnglish(t *testing.T) {
	for _, l := range []string{"zh_cn", "ja_jp", "hi_in"} {
		if !NeedsEnglish(l) {
			t.Errorf("%s needs a font this screen mode lacks", l)
		}
	}
	for _, l := range []string{"en_us", "pt_br", "es_es", "ru_ru", "xx_yy", ""} {
		if NeedsEnglish(l) {
			t.Errorf("%s must be drawn as it is", l)
		}
	}
}
