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

// Server is the Fairway HTTP API server.
type Server struct {
	store  Store
	logger *slog.Logger
	mux    *http.ServeMux
}

// New creates a Server and registers all routes.
func New(st Store, logger *slog.Logger) *Server {
	s := &Server{store: st, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler so *Server can be passed to http.ListenAndServe.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /corridors", s.handleListCorridors)
	s.mux.HandleFunc("GET /corridors/{id}/history", s.handleCorridorHistory)
}

// handleHealthz responds 200 OK when the service is alive and can reach the DB.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.logger.Warn("healthz db ping failed", "err", err)
		http.Error(w, `{"status":"error","detail":"db unreachable"}`, http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListCorridors returns all enabled corridors.
func (s *Server) handleListCorridors(w http.ResponseWriter, r *http.Request) {
	corridors, err := s.store.ListCorridors(r.Context())
	if err != nil {
		s.logger.Error("list corridors", "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"corridors": corridors})
}

// handleCorridorHistory returns paginated measurements for a single corridor.
// Query params: limit (default 50, max 200), offset (default 0).
func (s *Server) handleCorridorHistory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid corridor id"}`, http.StatusBadRequest)
		return
	}

	limit, err := queryInt(r, "limit", 50)
	if err != nil || limit <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be greater than 0"})
		return
	}
	if limit > 200 {
		limit = 200
	}

	offset, err := queryInt(r, "offset", 0)
	if err != nil || offset < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be greater than or equal to 0"})
		return
	}

	corridor, err := s.store.GetCorridor(r.Context(), id)
	if err != nil {
		s.logger.Error("get corridor", "id", id, "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if corridor == nil {
		http.Error(w, `{"error":"corridor not found"}`, http.StatusNotFound)
		return
	}

	measurements, err := s.store.ListMeasurements(r.Context(), id, limit, offset)
	if err != nil {
		s.logger.Error("list measurements", "corridor_id", id, "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"corridor":     corridor,
		"measurements": measurements,
		"limit":        limit,
		"offset":       offset,
	})
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
