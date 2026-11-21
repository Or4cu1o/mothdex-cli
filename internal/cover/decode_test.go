package cover

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecodeKeepsTheFetchLimits(t *testing.T) {
	var j bytes.Buffer
	if err := jpeg.Encode(&j, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	var g bytes.Buffer
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		data []byte
		ct   string
		ok   bool
	}{
		"png":                  {pngOf(t, 10, 10), "image/png", true},
		"jpeg":                 {j.Bytes(), "image/jpeg; charset=x", true},
		"gif":                  {g.Bytes(), "image/gif", true},
		"type disagrees":       {pngOf(t, 10, 10), "image/jpeg", false},
		"svg":                  {[]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), "image/svg+xml", false},
		"over the side cap":    {pngOf(t, MaxPixelSide+1, 1), "image/png", false},
		"garbage after header": {append(pngOf(t, 10, 10)[:30], "xx"...), "image/png", false},
		"over 5 MiB":           {append(pngOf(t, 10, 10), make([]byte, MaxBytes)...), "image/png", false},
		"empty":                {nil, "image/png", false},
		"gif frame over cap":   {gifWithFrame(MaxPixelSide + 1), "image/gif", false},
	} {
		if _, err := Decode(bytes.NewReader(c.data), c.ct, 0); (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", name, err, c.ok)
		}
	}
}

// gifWithFrame is a GIF whose logical screen is 2x2 and whose first frame is n by n: the screen size
// says nothing about the pixels the decoder allocates.
func gifWithFrame(n int) []byte {
	b := []byte("GIF89a")
	b = append(b, 2, 0, 2, 0, 0x80, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff) // screen, 2-color table
	lo, hi := byte(n&0xff), byte(n>>8&0xff)                          // n is at most 4097
	b = append(b, 0x2c, 0, 0, 0, 0, lo, hi, lo, hi, 0)
	b = append(b, 2, 2, 0x44, 0x01, 0, 0x3b) // minimal LZW data and the trailer
	return b
}

func TestDecodeScalesDownOnly(t *testing.T) {
	src := pngOf(t, 400, 200)
	img, err := Decode(bytes.NewReader(src), "image/png", 100)
	if err != nil || img.Bounds().Dx() != 100 || img.Bounds().Dy() != 50 {
		t.Fatalf("%v %v", img, err)
	}
	if px := img.At(10, 10); px != (color.NRGBA{0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("an opaque white source stays white: %v", px)
	}
	if img, err = Decode(bytes.NewReader(src), "image/png", 1000); err != nil || img.Bounds().Dx() != 400 {
		t.Fatalf("never scaled up: %v %v", img, err)
	}
}

// A 1x1 lossless WebP decodes through x/image/webp (single frame).
func TestDecodeWebP(t *testing.T) {
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(b), "image/webp", 0)
	if err != nil || img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatalf("%v %v", img, err)
	}
}
