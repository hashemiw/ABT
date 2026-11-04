package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"time"
)

// Mock modes. Only the mock reads them; real adapters ignore them.
const (
	ModeSuccess   = "success"
	ModeTimeout   = "timeout" // blocks until ctx ends, like a hung vendor
	ModeFailure   = "failure" // permanent provider failure
	ModeBadOut    = "invalid_output"
	ModeIdentical = "identical" // returns the source unchanged
	ModeBusy      = "busy"      // NotExecuted: definitely retryable
)

// Mock is offline and deterministic. It only proves the pipeline works: it does NOT edit clothing.
// Output = source tinted by a preset-dependent colour with a solid "MOCK" banner, so it visibly differs from the input.
type Mock struct {
	Mode  string
	Delay time.Duration
}

func NewMock(mode string) *Mock {
	if mode == "" {
		mode = ModeSuccess
	}
	return &Mock{Mode: mode}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Edit(ctx context.Context, in Input) (*Output, error) {
	start := time.Now()
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return nil, &Error{Uncertain, "PROVIDER_TIMEOUT", "mock cancelled during delay"}
		}
	}
	switch m.Mode {
	case ModeTimeout:
		<-ctx.Done()
		return nil, &Error{Uncertain, "PROVIDER_TIMEOUT", "mock timeout"}
	case ModeFailure:
		return nil, &Error{Permanent, "PROVIDER_FAILURE", "mock failure"}
	case ModeBusy:
		return nil, &Error{NotExecuted, "PROVIDER_UNAVAILABLE", "mock busy"}
	case ModeBadOut:
		return &Output{Image: []byte("not an image"), MIME: "image/jpeg", Provider: "mock", Model: "mock-v1", Mock: true}, nil
	case ModeIdentical:
		return &Output{Image: in.Image, MIME: in.MIME, Provider: "mock", Model: "mock-v1", Mock: true}, nil
	}
	src, _, err := image.Decode(bytes.NewReader(in.Image))
	if err != nil {
		return nil, &Error{Permanent, "PROVIDER_FAILURE", "mock cannot decode input"}
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	h := sha256.Sum256([]byte(in.Prompt)) // deterministic: same prompt -> same tint
	tint := color.RGBA{h[0], h[1], h[2], 90}
	draw.Draw(dst, b, &image.Uniform{tint}, image.Point{}, draw.Over)
	band := image.Rect(b.Min.X, b.Max.Y-b.Dy()/6, b.Max.X, b.Max.Y)
	draw.Draw(dst, band, &image.Uniform{color.RGBA{20, 20, 20, 255}}, image.Point{}, draw.Src)
	// Diagonal stripes inside the banner make it unmistakably synthetic.
	for y := band.Min.Y; y < band.Max.Y; y++ {
		for x := band.Min.X; x < band.Max.X; x++ {
			if (x+y)/12%2 == 0 {
				dst.Set(x, y, color.RGBA{250, 200, 0, 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, &Error{Permanent, "PROVIDER_FAILURE", "mock encode failed"}
	}
	return &Output{Image: buf.Bytes(), MIME: "image/jpeg", Provider: "mock", Model: "mock-v1", Mock: true, Latency: time.Since(start)}, nil
}
