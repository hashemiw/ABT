package media

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"net/http"

	"abt/backend/internal/apperr"
)

func invalid(msg string) *apperr.Error {
	return apperr.New(http.StatusUnprocessableEntity, "MEDIA_INVALID", msg)
}

// Normalize decodes untrusted bytes under pixel/size limits and re-encodes them as a
// metadata-free JPEG. File type comes from the content, never the declared type.
// shortcut: EXIF orientation is dropped, not applied; the iOS client uploads upright pixels.
// HEIC has no stdlib decoder, so the client converts to JPEG before upload.
func Normalize(raw []byte, maxPixels int64, minSide int) ([]byte, int, int, *apperr.Error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, 0, 0, invalid("This file is not a supported JPEG or PNG image.")
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, 0, 0, invalid("This image has too many pixels. Please use a smaller photo.")
	}
	if cfg.Width < minSide || cfg.Height < minSide {
		return nil, 0, 0, invalid("This image is too small. Please use a larger photo.")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, invalid("This image could not be decoded.")
	}
	if format == "png" { // JPEG has no alpha: flatten onto white
		flat := image.NewRGBA(img.Bounds())
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		img = flat
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, 0, 0, invalid("This image could not be processed.")
	}
	return out.Bytes(), cfg.Width, cfg.Height, nil
}
