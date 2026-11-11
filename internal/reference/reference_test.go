package reference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetToCurrency(t *testing.T) {
	tests := []struct {
		asset string
		want  string
	}{
		// Existing 'C'-suffixed stablecoins
		{"NGNC", "NGN"},
		{"USDC", "USD"},
		{"EURC", "EUR"},
		{"GHSC", "GHS"},
		{"KESC", "KES"},
		{"XLM", "XLM"},
		{"BRLC", "BRL"},
		// 'T'-suffixed fiat tokens
		{"USDT", "USD"},
		{"EURT", "EUR"},
		{"IDRT", "IDR"},
		{"ARST", "ARS"},
		// Asset-specific fiat token codes
		{"AUDD", "AUD"},
		{"XSGD", "SGD"},
		{"BRLA", "BRL"},
		// Already-ISO codes pass through unchanged
		{"USD", "USD"},
		{"NGN", "NGN"},
		// Case-insensitivity and whitespace trimming
		{"usdc", "USD"},
		{"  EURC  ", "EUR"},
		{"UsDt", "USD"},
		// Unknown codes are returned uppercased and trimmed
		{"ZZZ", "ZZZ"},
		{"", ""},
	}

	for _, tt := range tests {
		got := AssetToCurrency(tt.asset)
		if got != tt.want {
			t.Errorf("AssetToCurrency(%q) = %q, want %q", tt.asset, got, tt.want)
		}
	}
}

func TestFrankfurterFetcher(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := r.URL.Query().Get("base")
		quotes := r.URL.Query().Get("quotes")
		if base == "ngn" && quotes == "usd" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"date":"2026-10-05","base":"NGN","quote":"USD","rate":0.00075}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	fetcher := NewFrankfurterFetcher(ts.URL)
	rate, src, err := fetcher.FetchRate(context.Background(), "NGN", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 0.00075 {
		t.Errorf("expected rate 0.00075, got %f", rate)
	}
	if src != "frankfurter.app" {
		t.Errorf("expected src 'frankfurter.app', got %s", src)
	}
}
