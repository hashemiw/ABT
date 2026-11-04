// modelcheck makes FREE read-only metadata calls (models.list / models.get). It generates nothing and sends no images.
// It reads GEMINI_API_KEY from the environment and never prints it. Usage: go run ./cmd/modelcheck [model-id]
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type model struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Version                    string   `json:"version"`
	InputTokenLimit            int      `json:"inputTokenLimit"`
	OutputTokenLimit           int      `json:"outputTokenLimit"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

func get(path string) (int, []byte) {
	req, _ := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/"+path, nil)
	req.Header.Set("x-goog-api-key", os.Getenv("GEMINI_API_KEY"))
	c := http.Client{Timeout: 20 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Println("network error:", strings.ReplaceAll(err.Error(), os.Getenv("GEMINI_API_KEY"), "<key>"))
		os.Exit(2)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b
}

func main() {
	if os.Getenv("GEMINI_API_KEY") == "" {
		fmt.Println("GEMINI_API_KEY is empty (run scripts/real-ai.sh key)")
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		id := strings.TrimPrefix(os.Args[1], "models/")
		code, b := get("models/" + url.PathEscape(id))
		var m model
		_ = json.Unmarshal(b, &m)
		if code != 200 {
			fmt.Printf("models.get %s -> HTTP %d: %s\n", id, code, errMsg(b))
			os.Exit(3)
		}
		fmt.Printf("%s | %s | in=%d out=%d | methods=%v\n", m.Name, m.DisplayName, m.InputTokenLimit, m.OutputTokenLimit, m.SupportedGenerationMethods)
		return
	}
	code, b := get("models?pageSize=200")
	if code != 200 {
		fmt.Printf("models.list -> HTTP %d: %s\n", code, errMsg(b))
		os.Exit(3)
	}
	var out struct{ Models []model }
	_ = json.Unmarshal(b, &out)
	for _, m := range out.Models {
		if n := strings.ToLower(m.Name + m.DisplayName); strings.Contains(n, "image") || strings.Contains(n, "banana") {
			fmt.Printf("%s | %s | methods=%v\n", m.Name, m.DisplayName, m.SupportedGenerationMethods)
		}
	}
}

func errMsg(b []byte) string {
	var e struct {
		Error struct {
			Code            int
			Status, Message string
		}
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return e.Error.Status + ": " + e.Error.Message
	}
	return "unparseable error body"
}
