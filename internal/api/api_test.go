package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofairway/fairway/internal/store"
)

type mockStore struct {
	corridors           []store.Corridor
	measurements        map[int][]store.Measurement
	pingErr             error
	listCorridorsErr    error
	getCorridorErr      error
	listMeasurementsErr error
}

func (m *mockStore) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockStore) ListCorridors(ctx context.Context) ([]store.Corridor, error) {
	if m.listCorridorsErr != nil {
		return nil, m.listCorridorsErr
	}
	return m.corridors, nil
}

func (m *mockStore) GetCorridor(ctx context.Context, id int) (*store.Corridor, error) {
	if m.getCorridorErr != nil {
		return nil, m.getCorridorErr
	}
	for _, c := range m.corridors {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ListMeasurements(ctx context.Context, corridorID, limit, offset int) ([]store.Measurement, error) {
	if m.listMeasurementsErr != nil {
		return nil, m.listMeasurementsErr
	}
	ms, ok := m.measurements[corridorID]
	if !ok {
		return []store.Measurement{}, nil
	}
	if offset >= len(ms) {
		return []store.Measurement{}, nil
	}
	end := offset + limit
	if end > len(ms) {
		end = len(ms)
	}
	return ms[offset:end], nil
}

func newTestServer(st Store) *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, logger)
}

func sampleCorridor() store.Corridor {
	now := time.Now().UTC().Truncate(time.Second)
	return store.Corridor{
		ID:                 1,
		Name:               "NGNC/USDC",
		SellAssetCode:      "NGNC",
		SellAssetIssuer:    "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
		BuyAssetCode:       "USDC",
		BuyAssetIssuer:     "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		SellDomain:         "ngnc.online",
		SellAnchorMetadata: "full",
		SellVerifiedStatus: "live",
		BuyDomain:          "centre.io",
		BuyAnchorMetadata:  "full",
		BuyVerifiedStatus:  "live",
		VerificationDate:   &now,
		TargetUSDValue:     100.0,
		Enabled:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestHandleCorridorHistory_NegativeOffset(t *testing.T) {
	st := &mockStore{
		corridors: []store.Corridor{sampleCorridor()},
	}
	srv := newTestServer(st)

	tests := []struct {
		name       string
		targetURL  string
		wantStatus int
		wantError  string
	}{
		{
			name:       "offset is -5",
			targetURL:  "/corridors/1/history?offset=-5",
			wantStatus: http.StatusBadRequest,
			wantError:  "offset must be greater than or equal to 0",
		},
		{
			name:       "offset is -1",
			targetURL:  "/corridors/1/history?offset=-1",
			wantStatus: http.StatusBadRequest,
			wantError:  "offset must be greater than or equal to 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.targetURL, nil)
			w := httptest.NewRecorder()

			srv.ServeHTTP(w, req)
			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decoding response body: %v", err)
			}

			if got := body["error"]; got != tt.wantError {
				t.Errorf("error message = %q, want %q", got, tt.wantError)
			}
		})
	}
}

func TestHandleCorridorHistory_ZeroOrNegativeLimit(t *testing.T) {
	st := &mockStore{
		corridors: []store.Corridor{sampleCorridor()},
	}
	srv := newTestServer(st)

	tests := []struct {
		name       string
		targetURL  string
		wantStatus int
		wantError  string
	}{
		{
			name:       "limit is 0",
			targetURL:  "/corridors/1/history?limit=0",
			wantStatus: http.StatusBadRequest,
			wantError:  "limit must be greater than 0",
		},
		{
			name:       "limit is -1",
			targetURL:  "/corridors/1/history?limit=-1",
			wantStatus: http.StatusBadRequest,
			wantError:  "limit must be greater than 0",
		},
		{
			name:       "limit is -50",
			targetURL:  "/corridors/1/history?limit=-50",
			wantStatus: http.StatusBadRequest,
			wantError:  "limit must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.targetURL, nil)
			w := httptest.NewRecorder()

			srv.ServeHTTP(w, req)
			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decoding response body: %v", err)
			}

			if got := body["error"]; got != tt.wantError {
				t.Errorf("error message = %q, want %q", got, tt.wantError)
			}
		})
	}
}

func TestHandleCorridorHistory_ValidBoundaries(t *testing.T) {
	c := sampleCorridor()
	m1 := store.Measurement{
		ID:             10,
		CorridorID:     1,
		MeasuredAt:     time.Now().UTC(),
		SellAmount:     100.0,
		IntegrityState: store.StateUsable,
		PathFound:      true,
	}

	st := &mockStore{
		corridors: []store.Corridor{c},
		measurements: map[int][]store.Measurement{
			1: {m1},
		},
	}
	srv := newTestServer(st)

	tests := []struct {
		name       string
		targetURL  string
		wantStatus int
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "limit boundary 1",
			targetURL:  "/corridors/1/history?limit=1",
			wantStatus: http.StatusOK,
			wantLimit:  1,
			wantOffset: 0,
		},
		{
			name:       "limit boundary 200",
			targetURL:  "/corridors/1/history?limit=200",
			wantStatus: http.StatusOK,
			wantLimit:  200,
			wantOffset: 0,
		},
		{
			name:       "offset boundary 0",
			targetURL:  "/corridors/1/history?offset=0",
			wantStatus: http.StatusOK,
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "limit 1 and offset 0",
			targetURL:  "/corridors/1/history?limit=1&offset=0",
			wantStatus: http.StatusOK,
			wantLimit:  1,
			wantOffset: 0,
		},
		{
			name:       "limit 200 and offset 0",
			targetURL:  "/corridors/1/history?limit=200&offset=0",
			wantStatus: http.StatusOK,
			wantLimit:  200,
			wantOffset: 0,
		},
		{
			name:       "default query params",
			targetURL:  "/corridors/1/history",
			wantStatus: http.StatusOK,
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "limit capped at 200",
			targetURL:  "/corridors/1/history?limit=250",
			wantStatus: http.StatusOK,
			wantLimit:  200,
			wantOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.targetURL, nil)
			w := httptest.NewRecorder()

			srv.ServeHTTP(w, req)
			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			var body struct {
				Corridor     store.Corridor      `json:"corridor"`
				Measurements []store.Measurement `json:"measurements"`
				Limit        int                 `json:"limit"`
				Offset       int                 `json:"offset"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decoding response body: %v", err)
			}

			if body.Limit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", body.Limit, tt.wantLimit)
			}
			if body.Offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", body.Offset, tt.wantOffset)
			}
			if body.Corridor.ID != 1 {
				t.Errorf("corridor ID = %d, want 1", body.Corridor.ID)
			}
		})
	}
}

func TestHandleCorridorHistory_InvalidIDAndNotFound(t *testing.T) {
	st := &mockStore{
		corridors: []store.Corridor{sampleCorridor()},
	}
	srv := newTestServer(st)

	t.Run("non-integer corridor id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/invalid/history", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("non-existent corridor id returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/999/history", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})
}

func TestHandleHealthz(t *testing.T) {
	t.Run("healthy db returns 200 ok", func(t *testing.T) {
		st := &mockStore{}
		srv := newTestServer(st)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("unreachable db returns 503", func(t *testing.T) {
		st := &mockStore{pingErr: errors.New("db down")}
		srv := newTestServer(st)
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", w.Code)
		}
	})
}

func TestHandleListCorridors(t *testing.T) {
	st := &mockStore{
		corridors: []store.Corridor{sampleCorridor()},
	}
	srv := newTestServer(st)
	req := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Corridors []store.Corridor `json:"corridors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if len(body.Corridors) != 1 {
		t.Fatalf("expected 1 corridor, got %d", len(body.Corridors))
	}
}

func TestQueryInt(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?limit=15&offset=3&empty=&invalid=xyz", nil)

	t.Run("valid key", func(t *testing.T) {
		n, err := queryInt(req, "limit", 50)
		if err != nil || n != 15 {
			t.Errorf("got (%d, %v), want (15, nil)", n, err)
		}
	})

	t.Run("missing key returns default", func(t *testing.T) {
		n, err := queryInt(req, "missing", 50)
		if err != nil || n != 50 {
			t.Errorf("got (%d, %v), want (50, nil)", n, err)
		}
	})

	t.Run("empty value returns default", func(t *testing.T) {
		n, err := queryInt(req, "empty", 50)
		if err != nil || n != 50 {
			t.Errorf("got (%d, %v), want (50, nil)", n, err)
		}
	})

	t.Run("invalid integer returns error", func(t *testing.T) {
		_, err := queryInt(req, "invalid", 50)
		if err == nil {
			t.Error("expected error for non-integer query param, got nil")
		}
	})
}
