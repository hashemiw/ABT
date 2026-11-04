package storage

import (
	"strings"
	"testing"
	"time"
)

// AWS's published SigV4 query-string example (S3 "Authenticating Requests: Using Query Parameters").
func TestPresignAWSVector(t *testing.T) {
	c := New(Config{Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"})
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	got := c.presignPath("GET", "/test.txt", map[string]string{"host": "examplebucket.s3.amazonaws.com"}, 86400*time.Second, now)
	want := "X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if !strings.Contains(got, want) {
		t.Fatalf("signature mismatch: %s", got)
	}
}

func TestPresignPutBindsSizeAndType(t *testing.T) {
	c := New(Config{PublicEndpoint: "http://127.0.0.1:9000", Bucket: "b", Region: "us-east-1", AccessKey: "a", SecretKey: "s"})
	u, h, _ := c.PresignPut("k/1", "image/jpeg", 123, time.Minute)
	if !strings.Contains(u, "X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost") || h["Content-Type"] != "image/jpeg" {
		t.Fatal(u)
	}
}
