package cover

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/gif"  // register GIF for DecodeConfig
	"image/jpeg" // register JPEG for DecodeConfig
	"image/png"  // register PNG for DecodeConfig
	"strings"
)

var _ = []any{gif.Decode, jpeg.Decode, png.Decode}

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

// webpSize parses the VP8 (lossy), VP8L (lossless) and VP8X (extended) headers.
func webpSize(b []byte) (int, int, error) {
	if len(b) < 30 {
		return 0, 0, errBadImage
	}
	switch string(b[12:16]) {
	case "VP8 ":
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0, errBadImage
		}
		return int(binary.LittleEndian.Uint16(b[26:])) & 0x3fff, int(binary.LittleEndian.Uint16(b[28:])) & 0x3fff, nil
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0, errBadImage
		}
		v := binary.LittleEndian.Uint32(b[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, nil
	case "VP8X":
		return 1 + (int(b[24]) | int(b[25])<<8 | int(b[26])<<16), 1 + (int(b[27]) | int(b[28])<<8 | int(b[29])<<16), nil
	}
	return 0, 0, errBadImage
}
