package httpapi

import (
	"net/http"

	"abt/backend/internal/apperr"
	"abt/backend/internal/generation"
)

func (s *server) listPresets(w http.ResponseWriter, r *http.Request) error {
	ps, err := s.Generation.ListPresets(r.Context())
	if err != nil {
		return err
	}
	data := make([]map[string]string, 0, len(ps))
	for _, p := range ps {
		data = append(data, map[string]string{"id": p.ID, "name": p.Name, "category": p.Category, "description": p.Description})
	}
	return writeJSON(w, 200, map[string]any{"data": data})
}

func (s *server) createGeneration(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		SourceAssetID      string  `json:"source_asset_id"`
		PresetID           string  `json:"preset_id"`
		Instruction        string  `json:"instruction"`
		ParentGenerationID *string `json:"parent_generation_id"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if !uuidRe.MatchString(in.SourceAssetID) || in.PresetID == "" {
		return apperr.New(http.StatusUnprocessableEntity, "INVALID_REQUEST", "source_asset_id and preset_id are required.")
	}
	c, err := s.Generation.Create(r.Context(), userID(r), r.Header.Get("Idempotency-Key"), generation.CreateRequest{
		SourceAssetID: in.SourceAssetID, PresetID: in.PresetID, Instruction: in.Instruction, ParentGenerationID: in.ParentGenerationID})
	if err != nil {
		return err
	}
	return writeJSON(w, 202, map[string]any{"id": c.ID, "status": c.Status, "created_at": rfc3339(c.CreatedAt), "status_url": "/v1/generations/" + c.ID})
}

func (s *server) getGeneration(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	v, err := s.Generation.Get(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	body := map[string]any{"id": v.ID, "status": v.Status, "preset_id": v.PresetID, "created_at": rfc3339(v.CreatedAt),
		"started_at": nil, "completed_at": nil, "result": nil, "error": nil}
	if v.StartedAt != nil {
		body["started_at"] = rfc3339(*v.StartedAt)
	}
	if v.CompletedAt != nil {
		body["completed_at"] = rfc3339(*v.CompletedAt)
	}
	if v.Result != nil {
		body["result"] = map[string]any{"asset_id": v.Result.AssetID, "download_url": v.Result.DownloadURL, "download_url_expires_at": rfc3339(v.Result.DownloadExpires),
			"provider": v.Result.Provider, "model": v.Result.Model, "is_mock": v.Result.Mock}
	}
	if v.Status == "failed" {
		code := v.ErrorCode
		if code == "" {
			code = "GENERATION_FAILED"
		}
		body["error"] = map[string]string{"code": code, "message": generation.SafeMessage(code)}
	}
	return writeJSON(w, 200, body)
}

func (s *server) cancelGeneration(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct{}
	if err = decode(r, &in); err != nil {
		return err
	}
	status, err := s.Generation.Cancel(r.Context(), userID(r), id)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]string{"id": id, "status": status})
}
