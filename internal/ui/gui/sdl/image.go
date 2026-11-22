//go:build gui

package sdl

import (
	"fmt"
	"image"

	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// maxImageTextures is how many cover textures stay in the video memory (docs/contracts/cli/gui.md section 5).
const maxImageTextures = 48

// argbFromRGBA converts a picture to the bytes of SDL_PIXELFORMAT_ARGB8888 on a little-endian CPU (B, G, R, A),
// with straight alpha: image.RGBA is premultiplied and SDL blends straight. It returns the bytes and the
// pitch of a row.
func argbFromRGBA(img *image.RGBA) ([]byte, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+w*4]
		dst := out[y*w*4 : (y+1)*w*4]
		for x := 0; x < w; x++ {
			r, g, bl, a := src[x*4], src[x*4+1], src[x*4+2], src[x*4+3]
			if a != 0 && a != 0xff { // un-premultiply
				r = uint8(min(uint16(r)*0xff/uint16(a), 0xff))   //nolint:gosec // clamped to a byte
				g = uint8(min(uint16(g)*0xff/uint16(a), 0xff))   //nolint:gosec // clamped to a byte
				bl = uint8(min(uint16(bl)*0xff/uint16(a), 0xff)) //nolint:gosec // clamped to a byte
			}
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = bl, g, r, a
		}
	}
	return out, w * 4
}

// drawImage draws a picture scaled into its rectangle, uploading it as a texture the first time its id is
// seen and reusing the texture from the cache after that.
func (b *Backend) drawImage(op gdraw.Op) error {
	tex, ok := b.images.get(op.ImageID)
	if !ok {
		var err error
		if tex, err = b.uploadImage(op.Pixels); err != nil {
			return err
		}
		b.images.put(op.ImageID, tex)
	}
	if err := b.setBlend(BlendNone); err != nil { // a cover is opaque; the blend mode of the texture decides
		return err
	}
	dst := toRect(op.Rect)
	return b.lib.Copy(b.ren, tex, nil, &dst)
}

func (b *Backend) uploadImage(img *image.RGBA) (Texture, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	tex, err := b.lib.CreateTexture(b.ren, PixelFormatARGB8888, TextureAccessStatic, int32(w), int32(h)) //nolint:gosec // a cover is at most 512 px a side
	if err != nil {
		return Texture{}, fmt.Errorf("cover texture: %w", err)
	}
	if err := b.lib.SetTextureBlend(tex, BlendBlend); err != nil {
		b.lib.DestroyTexture(tex)
		return Texture{}, err
	}
	if err := b.lib.SetTextureColor(tex, 0xff, 0xff, 0xff, 0xff); err != nil {
		b.lib.DestroyTexture(tex)
		return Texture{}, err
	}
	pixels, pitch := argbFromRGBA(img)
	if err := b.lib.UpdateTexture(tex, nil, pixels, int32(pitch)); err != nil { //nolint:gosec // at most 2 KiB
		b.lib.DestroyTexture(tex)
		return Texture{}, err
	}
	return tex, nil
}
