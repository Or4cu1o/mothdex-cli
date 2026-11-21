package cover

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"io"

	"golang.org/x/image/webp"
)

// ErrDecode is returned for a cover that is too big, of an unknown or disagreeing type, or unreadable.
var ErrDecode = errors.New("cover: cannot decode the image")

// Decode reads one cover from r and returns its first frame, scaled down to fit maxSide pixels on
// its longer side when maxSide is above zero (never scaled up). It applies the same limits as
// Fetch, so a hostile image from the cache is refused too: at most MaxBytes of input, a type from
// the allowlist whose magic bytes agree with contentType, and a header size of at most MaxPixelSide
// on each axis, checked before any pixel is decoded. Animated images yield their first frame.
func Decode(r io.Reader, contentType string, maxSide int) (image.Image, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil || len(b) > MaxBytes {
		return nil, ErrDecode
	}
	kind := sniff(b)
	if kind == "" || kind != typeOfMedia(contentType) {
		return nil, ErrDecode
	}
	w, h, err := dimensions(kind, b)
	if err != nil || w < 1 || h < 1 || w > MaxPixelSide || h > MaxPixelSide {
		return nil, ErrDecode
	}
	if kind == "gif" && !gifFramesFit(b) { // the header says nothing about the frame the decoder allocates
		return nil, ErrDecode
	}
	var img image.Image
	switch kind {
	case "webp":
		img, err = webp.Decode(bytes.NewReader(b))
	case "gif":
		img, err = gif.Decode(bytes.NewReader(b)) // the first frame only
	default:
		img, _, err = image.Decode(bytes.NewReader(b))
	}
	if err != nil {
		return nil, ErrDecode
	}
	return fit(img, maxSide), nil
}

// gifFramesFit walks the GIF blocks up to the first image descriptor and checks that the frame size
// is within MaxPixelSide: a small logical screen can carry a frame of up to 65535 by 65535 pixels.
func gifFramesFit(b []byte) bool {
	if len(b) < 13 {
		return false
	}
	off := 13
	if b[10]&0x80 != 0 { // global color table
		off += 3 << (1 + int(b[10]&7))
	}
	for off < len(b) {
		switch b[off] {
		case 0x21: // extension: label, then sub-blocks
			off += 2
			for off < len(b) && b[off] != 0 {
				off += 1 + int(b[off])
			}
			off++
		case 0x2c: // image descriptor: left, top, width, height
			if off+9 > len(b) {
				return false
			}
			fw, fh := int(b[off+5])|int(b[off+6])<<8, int(b[off+7])|int(b[off+8])<<8
			return fw >= 1 && fh >= 1 && fw <= MaxPixelSide && fh <= MaxPixelSide
		default: // trailer or garbage
			return false
		}
	}
	return false
}

// fit scales img down so that its longer side is at most maxSide, averaging the source pixels
// under each target pixel. maxSide <= 0 or an image that already fits is returned as is.
func fit(img image.Image, maxSide int) image.Image {
	bnd := img.Bounds()
	w, h := bnd.Dx(), bnd.Dy()
	if maxSide <= 0 || max(w, h) <= maxSide {
		return img
	}
	nw, nh := max(1, w*maxSide/max(w, h)), max(1, h*maxSide/max(w, h))
	src := image.NewNRGBA(bnd)
	draw.Draw(src, bnd, img, bnd.Min, draw.Src)
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, max(y*h/nh+1, (y+1)*h/nh)
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, max(x*w/nw+1, (x+1)*w/nw)
			var r, g, bl, a, n int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					o := src.PixOffset(bnd.Min.X+sx, bnd.Min.Y+sy)
					pa := int(src.Pix[o+3])
					r += int(src.Pix[o]) * pa
					g += int(src.Pix[o+1]) * pa
					bl += int(src.Pix[o+2]) * pa
					a += pa
					n++
				}
			}
			d := dst.PixOffset(x, y)
			if a > 0 { // alpha-weighted, so a transparent border does not darken the edge
				dst.Pix[d], dst.Pix[d+1], dst.Pix[d+2] = uint8(r/a), uint8(g/a), uint8(bl/a) //nolint:gosec // an average of bytes
			}
			dst.Pix[d+3] = uint8(a / n) //nolint:gosec // an average of bytes
		}
	}
	return dst
}
