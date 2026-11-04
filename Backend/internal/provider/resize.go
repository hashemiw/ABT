package provider

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
)

// DownscaleJPEG shrinks img so its longer side is at most maxSide (box filter) and re-encodes it as JPEG.
// It bounds input tokens/cost for paid providers. Images already small enough are returned unchanged.
func DownscaleJPEG(src []byte, maxSide int) ([]byte, error) {
	im, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	long := max(b.Dx(), b.Dy())
	if maxSide <= 0 || long <= maxSide {
		return src, nil
	}
	w, h := b.Dx()*maxSide/long, b.Dy()*maxSide/long
	w, h = max(w, 1), max(h, 1)
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*b.Dy()/h, max((y+1)*b.Dy()/h, y*b.Dy()/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*b.Dx()/w, max((x+1)*b.Dx()/w, x*b.Dx()/w+1)
			var r, g, bl, n int
			for yy := y0; yy < y1; yy++ {
				o := rgba.PixOffset(x0, yy)
				for xx := x0; xx < x1; xx++ {
					r += int(rgba.Pix[o])
					g += int(rgba.Pix[o+1])
					bl += int(rgba.Pix[o+2])
					n++
					o += 4
				}
			}
			d := dst.PixOffset(x, y)
			dst.Pix[d], dst.Pix[d+1], dst.Pix[d+2], dst.Pix[d+3] = uint8(r/n), uint8(g/n), uint8(bl/n), 255
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
