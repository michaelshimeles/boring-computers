package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

// Server wires the HTTP mux, auth middleware and JSON handlers.
type Server struct {
	cfg         Config
	mgr         *Manager
	mux         *http.ServeMux
	infer       *inferLimiter
	storage     *Storage      // nil when no S3 endpoint is configured
	volLimiter  *inferLimiter // per-IP volume-creation cap
	agentBudget *dailyLimit   // global daily cap on agent runs
	inferBudget *dailyLimit   // global daily cap on inference requests
}

// NewServer builds the router with all routes from the contract.
func NewServer(cfg Config, mgr *Manager) *Server {
	s := &Server{cfg: cfg, mgr: mgr, mux: http.NewServeMux(), infer: newInferLimiter(cfg.InferenceRatePerMin), volLimiter: newInferLimiter(cfg.VolumeRatePerMin), agentBudget: newDailyLimit(cfg.DailyAgentMax), inferBudget: newDailyLimit(cfg.DailyInferMax)}
	if st, err := newStorage(cfg); err != nil {
		log.Printf("storage disabled: %v", err)
	} else if st != nil {
		s.storage = st
		s.startVolumeGC()
		log.Printf("storage enabled (bucket=%s quota=%dMB)", cfg.S3Bucket, cfg.VolumeQuotaMB)
	}

	// Open route: health check (never requires auth).
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Caddy on-demand-TLS gate for preview subdomains (open, internal).
	s.mux.HandleFunc("GET /internal/tls-check", s.handleTLSCheck)

	// Inference gateway (OpenAI-compatible; keys are server-side, per-IP capped).
	s.mux.Handle("POST /v1/chat/completions", s.auth(http.HandlerFunc(s.handleChatCompletions)))
	s.mux.Handle("GET /v1/models", s.auth(http.HandlerFunc(s.handleModels)))

	// Authenticated /v1 routes.
	s.mux.Handle("POST /v1/machines", s.auth(http.HandlerFunc(s.handleCreate)))
	s.mux.Handle("GET /v1/machines", s.auth(http.HandlerFunc(s.handleList)))
	s.mux.Handle("GET /v1/machines/{id}", s.auth(http.HandlerFunc(s.handleGet)))
	s.mux.Handle("DELETE /v1/machines/{id}", s.auth(http.HandlerFunc(s.handleDelete)))
	s.mux.Handle("POST /v1/machines/{id}/branch", s.auth(http.HandlerFunc(s.handleBranch)))

	// File transfer over the guest serial console.
	s.mux.Handle("POST /v1/machines/{id}/upload", s.auth(http.HandlerFunc(s.handleUpload)))
	s.mux.Handle("GET /v1/machines/{id}/download", s.auth(http.HandlerFunc(s.handleDownload)))

	// Path-based preview: reverse-proxy a guest port (works over the tunnel /
	// without wildcard DNS). Any method, sub-paths, and WS upgrades.
	s.mux.Handle("/v1/machines/{id}/web/{port}/{path...}", s.auth(http.HandlerFunc(s.handleWebProxy)))

	// Persistent volumes (S3-backed). Registered only when storage is configured.
	if s.storage != nil {
		s.mux.Handle("POST /v1/volumes", s.auth(http.HandlerFunc(s.handleCreateVolume)))
		s.mux.Handle("GET /v1/volumes/{id}", s.auth(http.HandlerFunc(s.handleGetVolume)))
		s.mux.Handle("DELETE /v1/volumes/{id}", s.auth(http.HandlerFunc(s.handleDeleteVolume)))
		s.mux.Handle("GET /v1/volumes/{id}/files", s.auth(http.HandlerFunc(s.handleListVolumeFiles)))
		s.mux.Handle("PUT /v1/volumes/{id}/file", s.auth(http.HandlerFunc(s.handlePutVolumeFile)))
		s.mux.Handle("GET /v1/volumes/{id}/file", s.auth(http.HandlerFunc(s.handleGetVolumeFile)))
		s.mux.Handle("DELETE /v1/volumes/{id}/file", s.auth(http.HandlerFunc(s.handleDeleteVolumeFile)))
		// Save a machine's /root into a volume (attach is via POST /v1/machines {volume}).
		s.mux.Handle("POST /v1/machines/{id}/save", s.auth(http.HandlerFunc(s.handleSaveVolume)))
	}

	// WebSocket TTY + VNC. Auth is handled inside (accepts ?token= too).
	s.mux.HandleFunc("GET /v1/machines/{id}/tty", s.handleTTY)
	s.mux.HandleFunc("GET /v1/machines/{id}/vnc", s.handleVNC)

	// Debug/agent: capture a PNG screenshot of a desktop machine.
	s.mux.Handle("GET /v1/machines/{id}/screenshot", s.auth(http.HandlerFunc(s.handleScreenshot)))

	// Computer-use agent: drive a desktop machine toward a goal, streaming
	// narration over a WebSocket.
	s.mux.HandleFunc("GET /v1/machines/{id}/agent", s.handleAgent)

	// Terminal agent: drive a shell machine toward a goal by typing commands
	// into its serial console, streaming narration over a WebSocket.
	s.mux.HandleFunc("GET /v1/machines/{id}/shell-agent", s.runShellAgent)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Preview hosts (<id>--<port>.<base>) bypass the API entirely and reverse-
	// proxy straight to the guest's port.
	if id, port, ok := s.previewTarget(r.Host); ok {
		s.handlePreview(w, r, id, port)
		return
	}
	// CORS so a browser on the deployed site's origin can call this endpoint.
	if o := s.cfg.CORSOrigin; o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Add("Vary", "Origin")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// auth is middleware enforcing the Bearer token on /v1/* routes when a token is
// configured. It does not apply to /healthz.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized returns true if the request carries the correct token, or if no
// token is configured. Accepts both the Authorization header and ?token=.
func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			if strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) == s.cfg.Token {
				return true
			}
		}
	}
	if q := r.URL.Query().Get("token"); q != "" && q == s.cfg.Token {
		return true
	}
	return false
}

// ---- handlers ----

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat("/dev/kvm")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"machines": s.mgr.Count(),
		"kvm":      err == nil,
	})
}

type createRequest struct {
	Template   string `json:"template"`
	TTLSeconds int    `json:"ttl_seconds"`
	Net        bool   `json:"net"`    // request internet (forces a cold boot)
	Volume     string `json:"volume"` // restore this volume into /root on boot
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if r.Body != nil {
		// Ignore decode errors: an empty body means defaults.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Template == "" {
		req.Template = "python"
	}

	m, err := s.mgr.Create(req.Template, req.TTLSeconds, req.Net, clientIP(r, s.cfg.TrustProxy))
	if err != nil {
		if errors.Is(err, ErrTooManyMachines) || errors.Is(err, ErrRateLimited) {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": err.Error()})
			return
		}
		log.Printf("create failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	// Restore a volume's snapshot into /root before the user connects (best-effort).
	if req.Volume != "" && s.storage != nil {
		if err := s.attachVolume(m.ID, req.Volume); err != nil {
			log.Printf("machine %s: attach volume %s failed: %v", m.ID, req.Volume, err)
		}
	}
	writeJSON(w, http.StatusCreated, m.View())
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	views := s.mgr.List()
	writeJSON(w, http.StatusOK, map[string]any{"machines": views})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, ok := s.mgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.mgr.Destroy(id) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.mgr.Branch(id, clientIP(r, s.cfg.TrustProxy))
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		case errors.Is(err, ErrTooManyMachines), errors.Is(err, ErrRateLimited):
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": err.Error()})
		case errors.Is(err, ErrSnapshotUnavailable):
			writeJSON(w, http.StatusNotImplemented, map[string]any{"error": err.Error()})
		default:
			log.Printf("branch failed: %v", err)
			writeJSON(w, http.StatusNotImplemented, map[string]any{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusCreated, m.View())
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
