// Package provider isolates image-editing vendors behind one small interface.
package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"time"
)

type Input struct {
	Image     []byte // validated source bytes
	MIME      string // e.g. image/jpeg
	Prompt    string // final, versioned instruction built by the service
	RequestID string // our generation id, for correlation only
}

type Output struct {
	Image    []byte
	MIME     string
	Provider string // "mock" | "gemini"
	Model    string
	Mock     bool
	Usage    map[string]int // token counts etc. when the vendor reports them
	Latency  time.Duration
	RemoteID string
}

type ImageProvider interface {
	// Edit must honour ctx. It must never log image bytes, keys or signed URLs.
	Edit(ctx context.Context, in Input) (*Output, error)
	Name() string
}

// Kind classifies a failure for the worker's retry policy. Cost safety drives the split:
// only failures where the vendor definitely did not run the job are retryable.
type Kind int

const (
	// NotExecuted: rejected before processing (e.g. HTTP 429/503 from the vendor). Safe to retry.
	NotExecuted Kind = iota
	// Uncertain: timeout, connection lost after sending. The vendor may have processed (and billed). Never auto-retried.
	Uncertain
	// Permanent: refusal, safety block, no image, bad request, invalid output. Retrying would not help.
	Permanent
)

type Error struct {
	Kind Kind
	Code string // PROVIDER_FAILURE | PROVIDER_TIMEOUT | PROVIDER_REFUSED | PROVIDER_NO_IMAGE | PROVIDER_INVALID_OUTPUT | PROVIDER_UNAVAILABLE
	Msg  string // internal detail; never contains secrets/URLs/images
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func AsError(err error) *Error {
	var pe *Error
	if errors.As(err, &pe) {
		return pe
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Uncertain, "PROVIDER_TIMEOUT", "deadline exceeded"}
	}
	return &Error{Uncertain, "PROVIDER_FAILURE", "unclassified provider error"}
}

// ValidateOutput checks a provider image before anything is stored. Identical-to-input bytes are a controlled
// failure (the original must never be returned as a result). Differing bytes alone do not prove quality.
func ValidateOutput(out, src []byte, maxBytes int64, maxPixels int64) (string, error) {
	if len(out) == 0 || int64(len(out)) > maxBytes {
		return "", &Error{Permanent, "PROVIDER_INVALID_OUTPUT", "output empty or too large"}
	}
	if bytes.Equal(out, src) {
		return "", &Error{Permanent, "PROVIDER_INVALID_OUTPUT", "output is byte-identical to the source"}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || (format != "jpeg" && format != "png") {
		return "", &Error{Permanent, "PROVIDER_INVALID_OUTPUT", "output is not a decodable JPEG/PNG"}
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return "", &Error{Permanent, "PROVIDER_INVALID_OUTPUT", fmt.Sprintf("output dimensions out of range (%dx%d)", cfg.Width, cfg.Height)}
	}
	return "image/" + format, nil
}
