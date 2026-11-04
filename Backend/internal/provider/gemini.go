package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Gemini calls the Gemini API generateContent endpoint with an input photo plus a text instruction
// and reads the returned inlineData image. Docs (verified 2026-11-04):
// https://aistudio.google.com/api-reference/generate-content , https://aistudio.google.com/docs/image-generation
// It is only constructed when config validation has allowed real, paid calls.
type Gemini struct {
	apiKey, model, base string
	hc                  *http.Client
}

const geminiBase = "https://generativelanguage.googleapis.com"

func NewGemini(apiKey, model string) *Gemini { return newGemini(apiKey, model, geminiBase) }

func newGemini(apiKey, model, base string) *Gemini {
	// No client-level timeout: the worker's context is the single deadline.
	return &Gemini{apiKey: apiKey, model: model, base: base, hc: &http.Client{}}
}

func (g *Gemini) Name() string { return "gemini" }

type gPart struct {
	Text       string `json:"text,omitempty"`
	InlineData *struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	} `json:"inlineData,omitempty"`
}

type gResponse struct {
	Candidates []struct {
		Content *struct {
			Parts []gPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata *struct {
		Prompt     int `json:"promptTokenCount"`
		Candidates int `json:"candidatesTokenCount"`
		Total      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ResponseID   string `json:"responseId"`
	ModelVersion string `json:"modelVersion"`
}

func (g *Gemini) Edit(ctx context.Context, in Input) (*Output, error) {
	start := time.Now()
	body, _ := json.Marshal(map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{
			map[string]any{"text": in.Prompt},
			map[string]any{"inlineData": map[string]string{"mimeType": in.MIME, "data": base64.StdEncoding.EncodeToString(in.Image)}},
		}}},
		"generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}},
	})
	u := g.base + "/v1beta/models/" + url.PathEscape(g.model) + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Permanent, "PROVIDER_FAILURE", "bad request construction"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)
	resp, err := g.hc.Do(req)
	if err != nil {
		// Never include err text: net/http wraps the URL, which is fine here, but stay minimal. The request may have been sent.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, &Error{Uncertain, "PROVIDER_TIMEOUT", "request timed out or cancelled"}
		}
		return nil, &Error{Uncertain, "PROVIDER_FAILURE", "transport error after send"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &Error{Uncertain, "PROVIDER_TIMEOUT", "timed out reading response"}
		}
		return nil, &Error{Uncertain, "PROVIDER_FAILURE", "response read failed"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, classifyHTTP(resp.StatusCode)
	}
	return parseGemini(raw, g.model, time.Since(start))
}

// classifyHTTP: 429 and 503 are rejections before processing (safe to retry); other 5xx may have executed.
func classifyHTTP(status int) *Error {
	switch {
	case status == 429 || status == 503:
		return &Error{NotExecuted, "PROVIDER_UNAVAILABLE", fmt.Sprintf("http %d", status)}
	case status == 408 || status == 504 || status >= 500:
		return &Error{Uncertain, "PROVIDER_FAILURE", fmt.Sprintf("http %d", status)}
	default: // 400/401/403/404: our request, key or model is wrong
		return &Error{Permanent, "PROVIDER_FAILURE", fmt.Sprintf("http %d", status)}
	}
}

// parseGemini is pure so fixtures can exercise every response shape without a network.
func parseGemini(raw []byte, model string, latency time.Duration) (*Output, error) {
	var r gResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, &Error{Permanent, "PROVIDER_FAILURE", "response is not JSON"}
	}
	if r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
		return nil, &Error{Permanent, "PROVIDER_REFUSED", "prompt blocked: " + r.PromptFeedback.BlockReason}
	}
	if len(r.Candidates) == 0 {
		return nil, &Error{Permanent, "PROVIDER_NO_IMAGE", "no candidates"}
	}
	c := r.Candidates[0]
	switch c.FinishReason {
	case "", "STOP", "MAX_TOKENS":
	case "SAFETY", "IMAGE_SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "RECITATION", "IMAGE_RECITATION", "SPII":
		return nil, &Error{Permanent, "PROVIDER_REFUSED", "finishReason " + c.FinishReason}
	default:
		return nil, &Error{Permanent, "PROVIDER_NO_IMAGE", "finishReason " + c.FinishReason}
	}
	if c.Content != nil {
		for _, p := range c.Content.Parts {
			if p.InlineData == nil || p.InlineData.Data == "" {
				continue
			}
			img, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
			if err != nil {
				return nil, &Error{Permanent, "PROVIDER_INVALID_OUTPUT", "image data is not base64"}
			}
			out := &Output{Image: img, MIME: p.InlineData.MimeType, Provider: "gemini", Model: model, RemoteID: r.ResponseID, Latency: latency}
			if r.UsageMetadata != nil {
				out.Usage = map[string]int{"prompt_tokens": r.UsageMetadata.Prompt, "output_tokens": r.UsageMetadata.Candidates, "total_tokens": r.UsageMetadata.Total}
			}
			return out, nil
		}
	}
	return nil, &Error{Permanent, "PROVIDER_NO_IMAGE", "response contained no image part"}
}
