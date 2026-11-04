package media

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestNormalize(t *testing.T) {
	var j, p bytes.Buffer
	jpeg.Encode(&j, image.NewRGBA(image.Rect(0, 0, 200, 100)), nil)
	png.Encode(&p, image.NewNRGBA(image.Rect(0, 0, 150, 150)))
	for name, tc := range map[string]struct {
		in           []byte
		maxPixels    int64
		wantOK       bool
		wantW, wantH int
	}{
		"jpeg":        {j.Bytes(), 1e6, true, 200, 100},
		"png":         {p.Bytes(), 1e6, true, 150, 150},
		"text":        {[]byte("hello"), 1e6, false, 0, 0},
		"empty":       {nil, 1e6, false, 0, 0},
		"pixel limit": {j.Bytes(), 100, false, 0, 0},
		"truncated":   {j.Bytes()[:60], 1e6, false, 0, 0},
	} {
		out, w, h, err := Normalize(tc.in, tc.maxPixels, 64)
		if (err == nil) != tc.wantOK || (err == nil && (w != tc.wantW || h != tc.wantH)) {
			t.Errorf("%s: err=%v %dx%d", name, err, w, h)
		}
		if err == nil {
			if _, e := jpeg.Decode(bytes.NewReader(out)); e != nil {
				t.Errorf("%s: output not JPEG: %v", name, e)
			}
		} else if err.Status != 422 || err.Code != "MEDIA_INVALID" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, _, err := Normalize(j.Bytes(), 1e6, 300); err == nil {
		t.Error("min side not enforced")
	}
}
