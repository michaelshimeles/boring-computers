package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// validVolumePath rejects paths containing directory traversal components.
func validVolumePath(p string) bool {
	cleaned := path.Clean(strings.TrimPrefix(p, "/"))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") {
		return false
	}
	return cleaned != "" && cleaned != "."
}

// HTTP surface for persistent volumes. Volumes are addressed by an unguessable
// id (the capability); with no accounts yet, holding the id is holding access.

const volumeFileCap = 64 << 20 // 64 MiB per uploaded file

func newVolumeID() string {
	b := make([]byte, 5)
	rand.Read(b)
	return "vol-" + hex.EncodeToString(b)
}

func (s *Server) volumeTTL(sec int) time.Duration {
	if sec <= 0 {
		sec = s.cfg.VolumeTTLDefault
	}
	if sec > s.cfg.VolumeTTLMax {
		sec = s.cfg.VolumeTTLMax
	}
	return time.Duration(sec) * time.Second
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	if !s.volLimiter.allow(clientIP(r, s.cfg.TrustProxy)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "slow down — too many volumes from your address"})
		return
	}
	var req struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	m, err := s.storage.Create(newVolumeID(), s.volumeTTL(req.TTLSeconds))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "couldn't create the volume"})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleGetVolume(w http.ResponseWriter, r *http.Request) {
	m, err := s.storage.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such volume (it may have expired)"})
		return
	}
	files, used, _ := s.storage.ListFiles(m.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "created_at": m.CreatedAt, "expires_at": m.ExpiresAt,
		"quota_mb": m.QuotaMB, "used_bytes": used, "files": len(files),
	})
}

func (s *Server) handleListVolumeFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.storage.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such volume"})
		return
	}
	files, used, err := s.storage.ListFiles(id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "couldn't list files"})
		return
	}
	if files == nil {
		files = []VolumeFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "used_bytes": used})
}

func (s *Server) handlePutVolumeFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path required"})
		return
	}
	if !validVolumePath(p) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid path"})
		return
	}
	if _, err := s.storage.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such volume"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, volumeFileCap+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "couldn't read body"})
		return
	}
	if len(data) > volumeFileCap {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file too big (64 MiB max)"})
		return
	}
	if err := s.storage.PutFile(id, p, strings.NewReader(string(data)), int64(len(data))); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "the volume is full"})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "couldn't store the file"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": p, "bytes": len(data)})
}

func (s *Server) handleGetVolumeFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path required"})
		return
	}
	if !validVolumePath(p) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid path"})
		return
	}
	obj, err := s.storage.GetFile(id, p)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such file"})
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeName(p)+"\"")
	io.Copy(w, obj)
}

func (s *Server) handleDeleteVolumeFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path required"})
		return
	}
	if !validVolumePath(p) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid path"})
		return
	}
	if err := s.storage.DeleteFile(id, p); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "couldn't delete"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	if err := s.storage.Delete(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "couldn't delete the volume"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// startVolumeGC periodically deletes expired volumes.
func (s *Server) startVolumeGC() {
	if s.storage == nil {
		return
	}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			s.storage.GCExpired()
		}
	}()
}
