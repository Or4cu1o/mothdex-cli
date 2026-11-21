package cover

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"  // register GIF for DecodeConfig
	_ "image/jpeg" // register JPEG for DecodeConfig
	_ "image/png"  // register PNG for DecodeConfig
	"strings"
)

// sniff returns the image type named by the leading magic bytes: jpeg, png, gif, webp or "".
func sniff(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp"
	}
	return ""
}

// typeOfMedia maps a Content-Type to the type sniff must find; "" for anything else (SVG, HTML).
func typeOfMedia(contentType string) string {
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch mt {
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	}
	return ""
}

var errBadImage = errors.New("cover: unreadable image header")

// dimensions reads the pixel size from the header only, never decoding the pixels.
func dimensions(kind string, b []byte) (w, h int, err error) {
	if kind == "webp" {
		return webpSize(b)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, errBadImage
	}
	return cfg.Width, cfg.Height, nil
}

// webpSize walks the RIFF chunks. A VP8X canvas is not trusted alone: the first VP8 or VP8L
// frame chunk is read too (ICCP, ANIM and ALPH are skipped), a file without one is refused, and
// the result is the larger of canvas and frame on each axis, so a small canvas cannot hide a
// big frame.
func webpSize(b []byte) (int, int, error) {
	var cw, ch int
	for off := 12; off+8 <= len(b); {
		kind, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		data := b[off+8:]
		if size < 0 || size > len(data) {
			if kind != "VP8 " && kind != "VP8L" {
				return 0, 0, errBadImage
			}
			size = len(data) // a frame chunk may be cut: only its header is needed
		}
		switch kind {
		case "VP8X":
			if size < 10 {
				return 0, 0, errBadImage
			}
			cw = 1 + (int(data[4]) | int(data[5])<<8 | int(data[6])<<16)
			ch = 1 + (int(data[7]) | int(data[8])<<8 | int(data[9])<<16)
		case "VP8 ", "VP8L":
			w, h, err := frameSize(kind, data[:size])
			if err != nil || (cw > 0 && (w > cw || h > ch)) {
				return 0, 0, errBadImage
			}
			return max(w, cw), max(h, ch), nil
		}
		off += 8 + size + size&1
	}
	return 0, 0, errBadImage
}

func frameSize(kind string, d []byte) (int, int, error) {
	if kind == "VP8 " {
		if len(d) < 10 || d[3] != 0x9d || d[4] != 0x01 || d[5] != 0x2a {
			return 0, 0, errBadImage
		}
		return int(binary.LittleEndian.Uint16(d[6:])) & 0x3fff, int(binary.LittleEndian.Uint16(d[8:])) & 0x3fff, nil
	}
	if len(d) < 5 || d[0] != 0x2f {
		return 0, 0, errBadImage
	}
	v := binary.LittleEndian.Uint32(d[1:])
	return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, nil
}
