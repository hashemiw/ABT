// Package storage is a minimal path-style S3 client (AWS Signature V4) for a private
// bucket. It exists to avoid a large SDK dependency for six operations.
package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Endpoint, PublicEndpoint, Bucket, Region, AccessKey, SecretKey string
}

type Client struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

var ErrNotFound = errors.New("object not found")

func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, now: time.Now}
}

const unsigned = "UNSIGNED-PAYLOAD"

// PresignPut returns a URL that accepts exactly one PUT of exactly size bytes with the given Content-Type.
func (c *Client) PresignPut(key, contentType string, size int64, ttl time.Duration) (string, map[string]string, time.Time) {
	now := c.now().UTC()
	h := map[string]string{"content-length": strconv.FormatInt(size, 10), "content-type": contentType}
	u := c.presign("PUT", c.cfg.PublicEndpoint, key, h, ttl, now)
	return u, map[string]string{"Content-Type": contentType}, now.Add(ttl)
}

func (c *Client) PresignGet(key string, ttl time.Duration) (string, time.Time) {
	now := c.now().UTC()
	return c.presign("GET", c.cfg.PublicEndpoint, key, nil, ttl, now), now.Add(ttl)
}

func (c *Client) path(key string) string {
	if key == "" {
		return "/" + c.cfg.Bucket
	}
	return "/" + c.cfg.Bucket + "/" + key
}

func (c *Client) presign(method, endpoint, key string, extra map[string]string, ttl time.Duration, now time.Time) string {
	u, _ := url.Parse(endpoint)
	headers := map[string]string{"host": u.Host}
	for k, v := range extra {
		headers[k] = v
	}
	return endpoint + c.presignPath(method, c.path(key), headers, ttl, now)
}

// presignPath returns "<path>?<signed query>"; split out so tests can use AWS's published vectors.
func (c *Client) presignPath(method, path string, headers map[string]string, ttl time.Duration, now time.Time) string {
	names := sortedKeys(headers)
	date, stamp := now.Format("20060102"), now.Format("20060102T150405Z")
	scope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	q := url.Values{
		"X-Amz-Algorithm":     {"AWS4-HMAC-SHA256"},
		"X-Amz-Credential":    {c.cfg.AccessKey + "/" + scope},
		"X-Amz-Date":          {stamp},
		"X-Amz-Expires":       {strconv.Itoa(int(ttl.Seconds()))},
		"X-Amz-SignedHeaders": {strings.Join(names, ";")},
	}
	canon := canonical(method, path, q, headers, names, unsigned)
	q.Set("X-Amz-Signature", c.signature(date, stamp, scope, canon))
	return path + "?" + encodeQuery(q)
}

func (c *Client) signature(date, stamp, scope, canon string) string {
	sum := sha256.Sum256([]byte(canon))
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	k := mac([]byte("AWS4"+c.cfg.SecretKey), date)
	k = mac(k, c.cfg.Region)
	k = mac(k, "s3")
	k = mac(k, "aws4_request")
	return hex.EncodeToString(mac(k, toSign))
}

func mac(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func canonical(method, path string, q url.Values, headers map[string]string, names []string, payload string) string {
	var h strings.Builder
	for _, n := range names {
		h.WriteString(n + ":" + strings.TrimSpace(headers[n]) + "\n")
	}
	return method + "\n" + uriEncode(path, false) + "\n" + encodeQuery(q) + "\n" + h.String() + "\n" + strings.Join(names, ";") + "\n" + payload
}

func encodeQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, uriEncode(k, true)+"="+uriEncode(q.Get(k), true))
	}
	return strings.Join(parts, "&")
}

func uriEncode(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && !slash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// do issues a header-signed request to the internal endpoint.
func (c *Client) do(ctx context.Context, method, key string, body []byte, contentType string) (*http.Response, error) {
	u, err := url.Parse(c.cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	payload := hex.EncodeToString(sum[:])
	now := c.now().UTC()
	date, stamp := now.Format("20060102"), now.Format("20060102T150405Z")
	headers := map[string]string{"host": u.Host, "x-amz-content-sha256": payload, "x-amz-date": stamp}
	if contentType != "" {
		headers["content-type"] = contentType
	}
	names := sortedKeys(headers)
	scope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	path := c.path(key)
	sig := c.signature(date, stamp, scope, canonical(method, path, url.Values{}, headers, names, payload))
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+uriEncode(path, false), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		if k != "host" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", c.cfg.AccessKey, scope, strings.Join(names, ";"), sig))
	return c.http.Do(req)
}

// Head returns the object size, or ErrNotFound.
func (c *Client) Head(ctx context.Context, key string) (int64, error) {
	resp, err := c.do(ctx, http.MethodHead, key, nil, "")
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		return resp.ContentLength, nil
	case 404:
		return 0, ErrNotFound
	}
	return 0, fmt.Errorf("storage HEAD: status %d", resp.StatusCode)
}

// Get reads an object and fails if it exceeds limit bytes.
func (c *Client) Get(ctx context.Context, key string, limit int64) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, key, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("storage GET: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("storage object exceeds limit")
	}
	return b, nil
}

func (c *Client) Put(ctx context.Context, key, contentType string, body []byte) error {
	resp, err := c.do(ctx, http.MethodPut, key, body, contentType)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("storage PUT: status %d", resp.StatusCode)
	}
	return nil
}

// Delete is idempotent: a missing object is success.
func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, key, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 200 && resp.StatusCode != 404 {
		return fmt.Errorf("storage DELETE: status %d", resp.StatusCode)
	}
	return nil
}

// EnsureBucket creates the bucket if it does not exist (local development and tests).
func (c *Client) EnsureBucket(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPut, "", nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 409 {
		return fmt.Errorf("storage create bucket: status %d", resp.StatusCode)
	}
	return nil
}
