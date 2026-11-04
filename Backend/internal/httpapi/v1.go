package httpapi

import (
	"net/http"
	"regexp"
	"time"

	"abt/backend/internal/apperr"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// pathID returns the {id} path value; a malformed ID cannot exist, so it is a 404.
func pathID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		return "", apperr.NotFound
	}
	return id, nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *server) createSession(w http.ResponseWriter, r *http.Request) error {
	var in struct{}
	if err := decode(r, &in); err != nil {
		return err
	}
	if !s.sessionLimit.allow(clientIP(r)) {
		return apperr.New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many sessions requested. Please try again shortly.")
	}
	is, err := s.Identity.Issue(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 201, map[string]any{
		"user":    map[string]string{"id": is.UserID},
		"session": map[string]string{"token": is.Token, "expires_at": rfc3339(is.ExpiresAt)},
	})
}

func (s *server) createUploadIntent(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := s.Media.CreateUploadIntent(r.Context(), userID(r), in.Filename, in.ContentType, in.SizeBytes)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, map[string]any{"asset_id": u.AssetID, "upload": map[string]any{
		"url": u.URL, "method": "PUT", "headers": u.Headers, "expires_at": rfc3339(u.ExpiresAt)}})
}

func (s *server) completeMedia(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct{}
	if err = decode(r, &in); err != nil {
		return err
	}
	a, err := s.Media.Complete(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"id": a.ID, "status": a.Status, "width": a.Width, "height": a.Height, "content_type": a.ContentType})
}

func (s *server) getMedia(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a, err := s.Media.Get(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	body := map[string]any{"id": a.ID, "kind": a.Kind, "status": a.Status, "width": a.Width, "height": a.Height, "content_type": a.ContentType,
		"download_url": nil, "download_url_expires_at": nil}
	if a.DownloadURL != "" {
		body["download_url"], body["download_url_expires_at"] = a.DownloadURL, rfc3339(a.DownloadExpires)
	}
	return writeJSON(w, 200, body)
}

func (s *server) deleteMedia(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	status, err := s.Media.Delete(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	return writeJSON(w, 202, map[string]string{"id": id, "status": status})
}
