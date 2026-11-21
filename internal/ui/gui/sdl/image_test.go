//go:build gui

package sdl

import (
	"image"
	"image/color"
	"slices"
	"testing"
)

func TestARGBConversionOrdersBytesAndUnPremultiplies(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 50, G: 100, B: 150, A: 128}) // premultiplied: the straight color is about 100,200,255
	img.SetRGBA(2, 0, color.RGBA{})                              // fully transparent
	img.SetRGBA(0, 1, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	pix, pitch := argbFromRGBA(img)
	if pitch != 12 || len(pix) != 24 {
		t.Fatalf("pitch %d, %d bytes", pitch, len(pix))
	}
	if got := pix[0:4]; !slices.Equal(got, []byte{30, 20, 10, 255}) { // B, G, R, A
		t.Errorf("opaque pixel %v", got)
	}
	if got := pix[4:8]; got[3] != 128 || got[2] < 98 || got[2] > 101 || got[0] < 253 {
		t.Errorf("a premultiplied pixel must be un-premultiplied: %v", got)
	}
	if got := pix[8:12]; !slices.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("transparent %v", got)
	}
	if got := pix[12:16]; !slices.Equal(got, []byte{0, 0, 255, 255}) {
		t.Errorf("red %v", got)
	}
}

func TestARGBConversionHonorsAStrideLargerThanTheRow(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4)).SubImage(image.Rect(1, 1, 3, 3)).(*image.RGBA)
	img.SetRGBA(1, 1, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetRGBA(2, 2, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	pix, pitch := argbFromRGBA(img)
	if pitch != 8 || len(pix) != 16 {
		t.Fatalf("pitch %d, %d bytes for a 2x2 sub image", pitch, len(pix))
	}
	if !slices.Equal(pix[0:4], []byte{3, 2, 1, 255}) || !slices.Equal(pix[12:16], []byte{6, 5, 4, 255}) {
		t.Fatalf("sub image pixels %v", pix)
	}
}

func TestLRUEvictsTheLeastRecentlyUsed(t *testing.T) {
	var evicted []int
	c := newLRU(3, func(k int, _ string) { evicted = append(evicted, k) })
	c.put(1, "a")
	c.put(2, "b")
	c.put(3, "c")
	if _, ok := c.get(1); !ok { // 1 is now the most recent
		t.Fatal("1 missing")
	}
	c.put(4, "d") // evicts 2
	c.put(5, "e") // evicts 3
	if !slices.Equal(evicted, []int{2, 3}) || c.len() != 3 {
		t.Fatalf("evicted %v, %d held", evicted, c.len())
	}
	if _, ok := c.get(2); ok {
		t.Error("2 must be gone")
	}
	c.put(1, "A") // replacing does not evict
	if v, _ := c.get(1); v != "A" || len(evicted) != 2 {
		t.Errorf("replace: %q, evicted %v", v, evicted)
	}
	c.clear()
	if c.len() != 0 || len(evicted) != 5 {
		t.Errorf("clear: %d held, evicted %v", c.len(), evicted)
	}
}
