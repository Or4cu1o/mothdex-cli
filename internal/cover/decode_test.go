package cover

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
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
		"png":                   {pngOf(t, 10, 10), "image/png", true},
		"jpeg":                  {j.Bytes(), "image/jpeg; charset=x", true},
		"gif":                   {g.Bytes(), "image/gif", true},
		"type disagrees":        {pngOf(t, 10, 10), "image/jpeg", false},
		"svg":                   {[]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), "image/svg+xml", false},
		"over the side cap":     {pngOf(t, MaxPixelSide+1, 1), "image/png", false},
		"garbage after header":  {append(pngOf(t, 10, 10)[:30], "xx"...), "image/png", false},
		"over 5 MiB":            {append(pngOf(t, 10, 10), make([]byte, MaxBytes)...), "image/png", false},
		"empty":                 {nil, "image/png", false},
		"pixel budget":          {pngOf(t, 2049, 2048), "image/png", false},
		"at the pixel budget":   {pngOf(t, 2048, 2048), "image/png", true},
		"webp over side cap":    {riffWebP("VP8L", vp8lHeader(5000, 1)), "image/webp", false},
		"webp animated":         {animatedWebP(), "image/webp", false},
		"gif second frame huge": {gifSecondFrameHuge(t), "image/gif", true},
		"gif frame over cap":    {gifWithFrame(MaxPixelSide + 1), "image/gif", false},
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

// vp8lHeader is the 5-byte VP8L header of a w by h image.
func vp8lHeader(w, h int) []byte {
	b := []byte{0x2f, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:], uint32(w-1)|uint32(h-1)<<14) //nolint:gosec // test sizes are small
	return b
}

func le32(n int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(n)) //nolint:gosec // test sizes are small
	return b
}

func riffWebP(chunks ...any) []byte {
	var body []byte
	for i := 0; i+1 < len(chunks); i += 2 {
		data := chunks[i+1].([]byte)
		body = append(body, chunks[i].(string)...)
		body = append(body, le32(len(data))...)
		body = append(body, data...)
		if len(data)%2 == 1 {
			body = append(body, 0)
		}
	}
	out := append([]byte("RIFF"), le32(4+len(body))...)
	return append(append(out, "WEBP"...), body...)
}

// animatedWebP has the VP8X animation flag and ANMF chunks but no top-level VP8 or VP8L chunk.
func animatedWebP() []byte {
	vp8x := []byte{0x02, 0, 0, 0, 3, 0, 0, 3, 0, 0} // animation flag, canvas 4x4
	return riffWebP("VP8X", vp8x, "ANIM", make([]byte, 6), "ANMF", append(make([]byte, 16), riffWebP("VP8L", vp8lHeader(4, 4))[12:]...))
}

// gifSecondFrameHuge is a valid GIF whose first frame is small and whose second frame descriptor
// claims 65535 by 65535 pixels: Decode must return the first frame and never allocate the second.
func gifSecondFrameHuge(t *testing.T) []byte {
	t.Helper()
	var g bytes.Buffer
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	b := bytes.TrimSuffix(g.Bytes(), []byte{0x3b})
	b = append(b, 0x2c, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0, 2, 1, 0, 0x3b)
	return b
}

func TestDecodeAnimatedGIFGivesTheFirstFrame(t *testing.T) {
	img, err := Decode(bytes.NewReader(gifSecondFrameHuge(t)), "image/gif", 0)
	if err != nil || img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("%v %v", img, err)
	}
}

// Shrinking keeps transparency: an opaque red left half and a clear right half do not darken the
// red or turn the clear part opaque.
func TestDecodeScaleKeepsTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 0xff, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&b, "image/png", 50)
	if err != nil {
		t.Fatal(err)
	}
	if px := img.At(5, 10).(color.NRGBA); px.R < 0xf0 || px.G > 0x10 || px.A < 0xf0 {
		t.Fatalf("opaque red side: %v", px)
	}
	if px := img.At(45, 10).(color.NRGBA); px.A > 0x10 {
		t.Fatalf("clear side: %v", px)
	}
}
