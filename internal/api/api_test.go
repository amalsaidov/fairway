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

func newTestServer(st Store, configs ...Config) *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, logger, configs...)
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

func TestSecurityHeaders(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

	paths := []struct {
		path        string
		wantCacheCt string
	}{
		{"/healthz", "no-store, no-cache, must-revalidate"},
		{"/corridors", "no-cache, must-revalidate"},
		{"/corridors/1/history", "no-cache, must-revalidate"},
	}

	for _, p := range paths {
		t.Run(p.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p.path, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := resp.Header.Get("Content-Security-Policy"); got != "default-src 'none'; frame-ancestors 'none'" {
				t.Errorf("Content-Security-Policy = %q", got)
			}
			if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
			if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if got := resp.Header.Get("Cache-Control"); got != p.wantCacheCt {
				t.Errorf("Cache-Control = %q, want %q", got, p.wantCacheCt)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
		})
	}
}

func TestCORS_Default(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	// Default config has empty CORSAllowedOrigins
	srv := newTestServer(st)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	req.Header.Set("Origin", "https://malicious.example.com")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin by default, got %q", got)
	}
}

func TestCORS_AllowedOrigin(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st, Config{
		CORSAllowedOrigins: "https://gofairway.github.io, https://app.example.com",
	})
	defer srv.Close()

	t.Run("matching origin receives CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req.Header.Set("Origin", "https://gofairway.github.io")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://gofairway.github.io" {
			t.Errorf("Access-Control-Allow-Origin = %q, want https://gofairway.github.io", got)
		}
		if got := resp.Header.Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want Origin", got)
		}
	})

	t.Run("unlisted origin receives no CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req.Header.Set("Origin", "https://unauthorized.org")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("expected no CORS header for unlisted origin, got %q", got)
		}
	})

	t.Run("OPTIONS preflight returns 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/corridors", nil)
		req.Header.Set("Origin", "https://gofairway.github.io")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("preflight status = %d, want 204", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://gofairway.github.io" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
	})
}

func TestCORS_ExplicitWildcard(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st, Config{
		CORSAllowedOrigins: "*",
	})
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	req.Header.Set("Origin", "https://anywhere.org")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestMethodEnforcement(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

	disallowedMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, m := range disallowedMethods {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/corridors", nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405, got %d", resp.StatusCode)
			}
			if allow := resp.Header.Get("Allow"); allow != "GET, HEAD, OPTIONS" {
				t.Errorf("Allow header = %q, want 'GET, HEAD, OPTIONS'", allow)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decoding JSON error: %v", err)
			}
			if body["error"] != "method not allowed" {
				t.Errorf("error = %q, want 'method not allowed'", body["error"])
			}
		})
	}
}

func TestHeadMethod(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodHead, "/corridors", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestNotFound(t *testing.T) {
	st := &mockStore{}
	srv := newTestServer(st)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/unknown-endpoint", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["error"] != "not found" {
		t.Errorf("error = %q, want 'not found'", body["error"])
	}
}

func TestRateLimiting(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	// Burst of 2 tokens, 0.1 RPS (will not refill within the test)
	srv := newTestServer(st, Config{
		RateLimitEnabled: true,
		RateLimitRPS:     0.1,
		RateLimitBurst:   2,
	})
	defer srv.Close()

	clientIP := "192.0.2.1:1234"

	// Request 1: allowed
	req1 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	req1.RemoteAddr = clientIP
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("req 1: expected 200, got %d", w1.Code)
	}

	// Request 2: allowed
	req2 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	req2.RemoteAddr = clientIP
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("req 2: expected 200, got %d", w2.Code)
	}

	// Request 3: rate limited
	req3 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	req3.RemoteAddr = clientIP
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3: expected 429, got %d", w3.Code)
	}
	if retry := w3.Header().Get("Retry-After"); retry != "1" {
		t.Errorf("Retry-After = %q, want 1", retry)
	}

	var body map[string]string
	if err := json.NewDecoder(w3.Body).Decode(&body); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("error = %q, want 'rate limit exceeded'", body["error"])
	}

	// Another client IP should still be allowed
	reqOther := httptest.NewRequest(http.MethodGet, "/corridors", nil)
	reqOther.RemoteAddr = "198.51.100.1:4321"
	wOther := httptest.NewRecorder()
	srv.ServeHTTP(wOther, reqOther)
	if wOther.Code != http.StatusOK {
		t.Fatalf("other client: expected 200, got %d", wOther.Code)
	}
}

func TestRateLimiting_TrustProxy(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}

	t.Run("TrustProxy false ignores X-Forwarded-For", func(t *testing.T) {
		srv := newTestServer(st, Config{
			RateLimitEnabled: true,
			RateLimitRPS:     0.1,
			RateLimitBurst:   1,
			TrustProxy:       false,
		})
		defer srv.Close()

		// Request 1 with spoofed XFF
		req1 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req1.RemoteAddr = "10.0.0.1:1234"
		req1.Header.Set("X-Forwarded-For", "203.0.113.195")
		w1 := httptest.NewRecorder()
		srv.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("req 1: expected 200, got %d", w1.Code)
		}

		// Request 2 with different spoofed XFF but same RemoteAddr should be throttled
		req2 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req2.RemoteAddr = "10.0.0.1:1234"
		req2.Header.Set("X-Forwarded-For", "203.0.113.196")
		w2 := httptest.NewRecorder()
		srv.ServeHTTP(w2, req2)
		if w2.Code != http.StatusTooManyRequests {
			t.Fatalf("req 2: expected 429 when TrustProxy is false, got %d", w2.Code)
		}
	})

	t.Run("TrustProxy true respects X-Forwarded-For", func(t *testing.T) {
		srv := newTestServer(st, Config{
			RateLimitEnabled: true,
			RateLimitRPS:     0.1,
			RateLimitBurst:   1,
			TrustProxy:       true,
		})
		defer srv.Close()

		// Request 1 from proxy for client A
		req1 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req1.RemoteAddr = "10.0.0.1:1234"
		req1.Header.Set("X-Forwarded-For", "203.0.113.10")
		w1 := httptest.NewRecorder()
		srv.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("req 1: expected 200, got %d", w1.Code)
		}

		// Request 2 from proxy for client B should succeed
		req2 := httptest.NewRequest(http.MethodGet, "/corridors", nil)
		req2.RemoteAddr = "10.0.0.1:1234"
		req2.Header.Set("X-Forwarded-For", "203.0.113.20")
		w2 := httptest.NewRecorder()
		srv.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("req 2: expected 200 for separate client IP, got %d", w2.Code)
		}
	})
}

func TestHandleCorridorHistory_NegativeOffset(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

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
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

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
	defer srv.Close()

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
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

	t.Run("non-integer corridor id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/invalid/history", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("zero corridor id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/0/history", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("negative corridor id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/-1/history", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("excessively huge offset returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/corridors/1/history?offset=2000000", nil)
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
		defer srv.Close()
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
		defer srv.Close()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", w.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decoding JSON: %v", err)
		}
		if body["detail"] != "db unreachable" {
			t.Errorf("detail = %q, want 'db unreachable'", body["detail"])
		}
	})
}

func TestHandleListCorridors(t *testing.T) {
	st := &mockStore{corridors: []store.Corridor{sampleCorridor()}}
	srv := newTestServer(st)
	defer srv.Close()

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
