// Package api provides the Fairway REST API.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofairway/fairway/internal/store"
)

// Store defines the data access operations required by Server.
type Store interface {
	Ping(ctx context.Context) error
	ListCorridors(ctx context.Context) ([]store.Corridor, error)
	GetCorridor(ctx context.Context, id int) (*store.Corridor, error)
	ListMeasurements(ctx context.Context, corridorID, limit, offset int) ([]store.Measurement, error)
}

// Config configures Server security, CORS, and rate limiting options.
type Config struct {
	CORSAllowedOrigins string
	RateLimitEnabled   bool
	RateLimitRPS       float64
	RateLimitBurst     int
	TrustProxy         bool
}

// Server is the Fairway HTTP API server.
type Server struct {
	store          Store
	logger         *slog.Logger
	mux            *http.ServeMux
	allowedOrigins map[string]bool
	allowAllCORS   bool
	limiter        *RateLimiter
}

// New creates a Server, configures security middlewares, and registers all routes.
func New(st Store, logger *slog.Logger, configs ...Config) *Server {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}

	s := &Server{
		store:          st,
		logger:         logger,
		mux:            http.NewServeMux(),
		allowedOrigins: make(map[string]bool),
	}

	// Configure CORS allowlist
	if trimmed := strings.TrimSpace(cfg.CORSAllowedOrigins); trimmed != "" {
		if trimmed == "*" {
			s.allowAllCORS = true
		} else {
			for _, o := range strings.Split(trimmed, ",") {
				if norm := strings.TrimSpace(o); norm != "" {
					s.allowedOrigins[norm] = true
				}
			}
		}
	}

	// Configure Rate Limiting
	if cfg.RateLimitEnabled {
		s.limiter = NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, cfg.TrustProxy)
	}

	s.routes()
	return s
}

// Close releases any background resources such as the rate limiter eviction timer.
func (s *Server) Close() {
	if s.limiter != nil {
		s.limiter.Stop()
	}
}

// ServeHTTP handles incoming requests by applying security headers, CORS,
// method restrictions (405), rate limits (429), and routing (404).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Security Headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")

	// 2. CORS Handling
	origin := r.Header.Get("Origin")
	if origin != "" && (s.allowAllCORS || s.allowedOrigins[origin]) {
		if s.allowAllCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 3. Method Enforcement (Only GET and HEAD permitted on public read-only API)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 4. Rate Limiting
	if s.limiter != nil {
		clientIP := s.limiter.ClientIP(r)
		if !s.limiter.Allow(clientIP) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}

	// 5. Route Matching & 404 Check
	_, pattern := s.mux.Handler(r)
	if pattern == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /corridors", s.handleListCorridors)
	s.mux.HandleFunc("GET /corridors/{id}/history", s.handleCorridorHistory)
}

// handleHealthz responds 200 OK when the service is alive and can reach the DB.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if err := s.store.Ping(r.Context()); err != nil {
		s.logger.Warn("healthz db ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "detail": "db unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListCorridors returns all enabled corridors.
func (s *Server) handleListCorridors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	corridors, err := s.store.ListCorridors(r.Context())
	if err != nil {
		s.logger.Error("list corridors", "err", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"corridors": corridors})
}

// handleCorridorHistory returns paginated measurements for a single corridor.
// Query params: limit (default 50, max 200), offset (default 0).
func (s *Server) handleCorridorHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 || id > 1000000000 {
		writeError(w, http.StatusBadRequest, "invalid corridor id")
		return
	}

	limit, err := queryInt(r, "limit", 50)
	if err != nil || limit <= 0 {
		writeError(w, http.StatusBadRequest, "limit must be greater than 0")
		return
	}
	if limit > 200 {
		limit = 200
	}

	offset, err := queryInt(r, "offset", 0)
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "offset must be greater than or equal to 0")
		return
	}
	if offset > 1000000 {
		writeError(w, http.StatusBadRequest, "offset is too large")
		return
	}

	corridor, err := s.store.GetCorridor(r.Context(), id)
	if err != nil {
		s.logger.Error("get corridor", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if corridor == nil {
		writeError(w, http.StatusNotFound, "corridor not found")
		return
	}

	measurements, err := s.store.ListMeasurements(r.Context(), id, limit, offset)
	if err != nil {
		s.logger.Error("list measurements", "corridor_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"corridor":     corridor,
		"measurements": measurements,
		"limit":        limit,
		"offset":       offset,
	})
}

// writeError serializes an error message into a standardized JSON response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// At this point the header is already sent; log only.
		_ = err
	}
}

// queryInt reads an integer query parameter with a default value.
// It returns an error if the parameter is present but cannot be parsed as an integer.
func queryInt(r *http.Request, key string, def int) (int, error) {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}
