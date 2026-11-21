package cover

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"io"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// MaxDecodePixels is the pixel budget of Decode, checked from the header: the side cap alone still
// allows 16 million pixels, which costs 64 to 190 MiB to decode (more for 16-bit PNG and progressive
// JPEG), too much for a handheld. A cover shown on screen does not need more than 4 megapixels.
const MaxDecodePixels = 4 << 20

// ErrDecode is returned for a cover that is too big, of an unknown or disagreeing type, or unreadable.
var ErrDecode = errors.New("cover: cannot decode the image")

// Decode reads one cover from r and returns its first frame, scaled down to fit maxSide pixels on
// its longer side when maxSide is above zero (never scaled up). It applies the same limits as
// Fetch plus a pixel budget (MaxDecodePixels), so a hostile image from the cache is refused too: at most MaxBytes of input, a type from
// the allowlist whose magic bytes agree with contentType, and a header size of at most MaxPixelSide
// on each axis, checked before any pixel is decoded. Animated images yield their first frame.
func Decode(r io.Reader, contentType string, maxSide int) (img image.Image, err error) {
	defer func() { // a decoder panic must not take a front end down
		if recover() != nil {
			img, err = nil, ErrDecode
		}
	}()
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil || len(b) > MaxBytes {
		return nil, ErrDecode
	}
	kind := sniff(b)
	if kind == "" || kind != typeOfMedia(contentType) {
		return nil, ErrDecode
	}
	w, h, err := dimensions(kind, b)
	if err != nil || w < 1 || h < 1 || w > MaxPixelSide || h > MaxPixelSide || w*h > MaxDecodePixels {
		return nil, ErrDecode
	}
	if kind == "gif" && !gifFramesFit(b) { // the header says nothing about the frame the decoder allocates
		return nil, ErrDecode
	}
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

// fit scales img down so that its longer side is at most maxSide, straight from the decoded image
// (no full copy of the source). maxSide <= 0 or an image that already fits is returned as is.
func fit(img image.Image, maxSide int) image.Image {
	bnd := img.Bounds()
	w, h := bnd.Dx(), bnd.Dy()
	if maxSide <= 0 || max(w, h) <= maxSide {
		return img
	}
	nw, nh := max(1, w*maxSide/max(w, h)), max(1, h*maxSide/max(w, h))
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, bnd, xdraw.Src, nil) // a filter scaled to the ratio: no aliasing when shrinking a lot
	return dst
}
