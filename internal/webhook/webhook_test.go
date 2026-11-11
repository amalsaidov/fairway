package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofairway/fairway/internal/store"
)

func TestDispatcher_NoURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher("", FormatRaw, logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		ID:         1,
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateDegraded,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err != nil {
		t.Fatalf("expected nil error when webhook URL is empty, got: %v", err)
	}
}

func TestDispatcher_Success(t *testing.T) {
	var receivedPayload Payload
	var receivedContentType string
	var receivedMethod string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, FormatRaw, logger)

	measurementID := int64(42)
	now := time.Now().Truncate(time.Millisecond)
	corridor := store.Corridor{ID: 10, Name: "USDC/EURC"}
	event := store.StateChangeEvent{
		ID:            5,
		CorridorID:    10,
		FromState:     store.StateDegraded,
		ToState:       store.StateUsable,
		MeasurementID: &measurementID,
		OccurredAt:    now,
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err != nil {
		t.Fatalf("unexpected error from Dispatch: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected HTTP POST, got %s", receivedMethod)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected application/json Content-Type, got %s", receivedContentType)
	}
	if receivedPayload.Event != "corridor.state_change" {
		t.Errorf("expected event 'corridor.state_change', got %s", receivedPayload.Event)
	}
	if receivedPayload.CorridorID != 10 {
		t.Errorf("expected corridor_id 10, got %d", receivedPayload.CorridorID)
	}
	if receivedPayload.CorridorName != "USDC/EURC" {
		t.Errorf("expected corridor_name 'USDC/EURC', got %s", receivedPayload.CorridorName)
	}
	if receivedPayload.FromState != "degraded" || receivedPayload.ToState != "usable" {
		t.Errorf("expected from=degraded, to=usable, got from=%s, to=%s", receivedPayload.FromState, receivedPayload.ToState)
	}
	if receivedPayload.MeasurementID == nil || *receivedPayload.MeasurementID != 42 {
		t.Errorf("expected measurement_id 42, got %v", receivedPayload.MeasurementID)
	}
}

func TestDispatcher_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, FormatRaw, logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUnknown,
		ToState:    store.StateUsable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err == nil {
		t.Fatal("expected error on HTTP 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected error message to mention 'HTTP 500', got: %v", err)
	}
}

func TestDispatcher_NetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close() // Close immediately to force connection failure

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, FormatRaw, logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateUnusable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err == nil {
		t.Fatal("expected network error on unreachable server, got nil")
	}
}

func TestDispatcher_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, FormatRaw, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateUnusable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(ctx, corridor, event)
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

func TestDispatcher_DiscordFormat(t *testing.T) {
	var rawBody []byte
	var receivedContentType string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		rawBody = b
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, FormatDiscord, logger)

	now := time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC)
	corridor := store.Corridor{ID: 7, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		ID:         3,
		CorridorID: 7,
		FromState:  store.StateUsable,
		ToState:    store.StateUnusable,
		OccurredAt: now,
	}

	if err := d.Dispatch(context.Background(), corridor, event); err != nil {
		t.Fatalf("unexpected error from Dispatch: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected application/json Content-Type, got %s", receivedContentType)
	}

	// The body must NOT contain the raw Payload's event field.
	if strings.Contains(string(rawBody), "corridor.state_change") {
		t.Errorf("discord payload unexpectedly contains raw Payload fields: %s", rawBody)
	}

	var dp discordPayload
	if err := json.Unmarshal(rawBody, &dp); err != nil {
		t.Fatalf("discord payload is not valid JSON: %v (body: %s)", err, rawBody)
	}
	if len(dp.Embeds) != 1 {
		t.Fatalf("expected exactly 1 embed, got %d", len(dp.Embeds))
	}

	embed := dp.Embeds[0]
	if embed.Color != discordColorUnusable {
		t.Errorf("expected unusable color %d, got %d", discordColorUnusable, embed.Color)
	}
	if !strings.Contains(embed.Title, "NGNC/USDC") || !strings.Contains(embed.Title, "unusable") {
		t.Errorf("unexpected embed title: %q", embed.Title)
	}
	if !strings.Contains(embed.Description, "usable") || !strings.Contains(embed.Description, "unusable") {
		t.Errorf("unexpected embed description: %q", embed.Description)
	}
	if embed.Timestamp != "2026-10-05T12:30:00Z" {
		t.Errorf("expected RFC3339 timestamp, got %q", embed.Timestamp)
	}
	if len(embed.Fields) != 3 {
		t.Fatalf("expected 3 embed fields, got %d", len(embed.Fields))
	}

	fields := map[string]string{}
	for _, f := range embed.Fields {
		fields[f.Name] = f.Value
	}
	if fields["Corridor"] != "NGNC/USDC" {
		t.Errorf("expected Corridor field 'NGNC/USDC', got %q", fields["Corridor"])
	}
	if fields["From"] != "usable" {
		t.Errorf("expected From field 'usable', got %q", fields["From"])
	}
	if fields["To"] != "unusable" {
		t.Errorf("expected To field 'unusable', got %q", fields["To"])
	}
}

func TestDispatcher_DiscordColors(t *testing.T) {
	cases := []struct {
		state store.IntegrityState
		color int
	}{
		{store.StateUsable, discordColorUsable},
		{store.StateDegraded, discordColorDegraded},
		{store.StateUnusable, discordColorUnusable},
		{store.StateUnknown, discordColorUnknown},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			var rawBody []byte
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				rawBody = b
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d := NewDispatcher(ts.URL, FormatDiscord, logger)

			corridor := store.Corridor{ID: 1, Name: "USDC/EURC"}
			event := store.StateChangeEvent{
				CorridorID: 1,
				FromState:  store.StateUnknown,
				ToState:    tc.state,
				OccurredAt: time.Now(),
			}
			if err := d.Dispatch(context.Background(), corridor, event); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var dp discordPayload
			if err := json.Unmarshal(rawBody, &dp); err != nil {
				t.Fatalf("invalid discord JSON: %v", err)
			}
			if len(dp.Embeds) != 1 {
				t.Fatalf("expected 1 embed, got %d", len(dp.Embeds))
			}
			if dp.Embeds[0].Color != tc.color {
				t.Errorf("state %s: expected color %d, got %d", tc.state, tc.color, dp.Embeds[0].Color)
			}
		})
	}
}

func TestNewDispatcher_UnknownFormatDefaultsToRaw(t *testing.T) {
	var receivedPayload Payload

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// An unrecognised format must fall back to the raw Payload encoding.
	d := NewDispatcher(ts.URL, Format("bogus"), logger)

	corridor := store.Corridor{ID: 2, Name: "USDC/EURC"}
	event := store.StateChangeEvent{
		CorridorID: 2,
		FromState:  store.StateUsable,
		ToState:    store.StateDegraded,
		OccurredAt: time.Now(),
	}
	if err := d.Dispatch(context.Background(), corridor, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPayload.Event != "corridor.state_change" {
		t.Errorf("expected raw Payload with event 'corridor.state_change', got %q", receivedPayload.Event)
	}
	if receivedPayload.CorridorName != "USDC/EURC" {
		t.Errorf("expected corridor_name 'USDC/EURC', got %q", receivedPayload.CorridorName)
	}
}
