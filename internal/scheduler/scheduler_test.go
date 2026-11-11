package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofairway/fairway/internal/measure"
	"github.com/gofairway/fairway/internal/reference"
	"github.com/gofairway/fairway/internal/store"
	"github.com/gofairway/fairway/internal/webhook"
)

func getTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		for _, u := range []string{
			"postgres://fairway:fairway@localhost:5433/fairway?sslmode=disable",
			"postgres://fairway:fairway@localhost:5432/fairway?sslmode=disable",
		} {
			s, err := store.New(u)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err = s.Ping(ctx)
				cancel()
				if err == nil {
					return s
				}
				s.Close()
			}
		}
		t.Skip("PostgreSQL database not available; skipping scheduler integration test")
		return nil
	}

	s, err := store.New(dbURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		s.Close()
		t.Skipf("database at %s not reachable: %v", dbURL, err)
		return nil
	}
	return s
}

// TestScheduler_StateChangeRegression_0d2acf9 tests the critical fix in commit 0d2acf9:
// Previous measurement must be fetched BEFORE inserting the new measurement into the DB.
//
// If the bug from 0d2acf9 is present:
//  1. Cycle 1 inserts Measurement 1 -> state transition unknown -> usable recorded.
//  2. Cycle 2 inserts Measurement 2 FIRST, then queries LatestMeasurement.
//     LatestMeasurement returns Measurement 2 (itself!), triggering `prev.ID == measurementID`.
//     This resets prevState to StateUnknown, falsely detecting a state change on EVERY cycle,
//     even when the corridor state has not changed.
//
// Under correct behavior:
// Cycle 2 fetches LatestMeasurement BEFORE inserting Measurement 2.
// It retrieves Measurement 1 (state: usable). Since newState is also usable,
// prevState == newState -> NO state change event is recorded and NO webhook is dispatched.
func TestScheduler_StateChangeRegression_0d2acf9(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Mock Frankfurter API: 1 USD = 0.90 EUR
	mockFrankfurter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"date":"2026-10-07","base":"USD","quote":"EUR","rate":0.90}]`))
	}))
	defer mockFrankfurter.Close()

	// Mock Horizon API: allow dynamically switching destination amount
	var destAmount atomic.Value
	destAmount.Store("90.0000000") // 100 * 0.90 = 90 expected -> 0% loss (usable)

	mockHorizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := destAmount.Load().(string)
		resp := fmt.Sprintf(`{
			"_embedded": {
				"records": [{
					"source_amount": "100.0000000",
					"destination_amount": "%s",
					"path": []
				}]
			}
		}`, amount)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer mockHorizon.Close()

	// Mock Webhook Target
	var webhookCalls int64
	mockWebhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&webhookCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockWebhook.Close()

	// Create unique test corridor in database
	testCorridorName := fmt.Sprintf("REGR_0D2ACF9_%d", time.Now().UnixNano())
	corridorID, err := st.UpsertCorridor(ctx, store.Corridor{
		Name:            testCorridorName,
		SellAssetCode:   "USDC",
		SellAssetIssuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		BuyAssetCode:    "EURC",
		BuyAssetIssuer:  "GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2",
		TargetUSDValue:  100.0,
		Enabled:         true,
	})
	if err != nil {
		t.Fatalf("failed to create test corridor: %v", err)
	}

	corridor, err := st.GetCorridor(ctx, corridorID)
	if err != nil || corridor == nil {
		t.Fatalf("failed to fetch created corridor: %v", err)
	}

	// Initialize Scheduler
	refFetcher := reference.NewFrankfurterFetcher(mockFrankfurter.URL)
	horizonClient := measure.NewHorizonClient(mockHorizon.URL, refFetcher)
	dispatcher := webhook.NewDispatcher(mockWebhook.URL, webhook.FormatRaw, logger)
	sched := New(st, horizonClient, dispatcher, logger, 100.0, 10*time.Second, 2.5, 5.0)

	// ==========================================
	// CYCLE 1: First measurement ever
	// ==========================================
	sched.measureOne(ctx, *corridor)

	// Verify cycle 1 results:
	// - 1 measurement in DB
	// - Exactly 1 state change event (unknown -> usable)
	// - Exactly 1 webhook call
	mList1, err := st.ListMeasurements(ctx, corridorID, 10, 0)
	if err != nil || len(mList1) != 1 {
		t.Fatalf("cycle 1: expected 1 measurement, got %d (err: %v)", len(mList1), err)
	}
	if mList1[0].IntegrityState != store.StateUsable {
		t.Fatalf("cycle 1: expected StateUsable, got %s", mList1[0].IntegrityState)
	}

	events1, err := getCorridorEvents(ctx, st, corridorID)
	if err != nil {
		t.Fatalf("cycle 1: error fetching events: %v", err)
	}
	if len(events1) != 1 {
		t.Fatalf("cycle 1: expected exactly 1 event, got %d", len(events1))
	}
	if events1[0].FromState != store.StateUnknown || events1[0].ToState != store.StateUsable {
		t.Errorf("cycle 1: expected event unknown -> usable, got %s -> %s", events1[0].FromState, events1[0].ToState)
	}
	if calls := atomic.LoadInt64(&webhookCalls); calls != 1 {
		t.Errorf("cycle 1: expected 1 webhook call, got %d", calls)
	}

	// ==========================================
	// CYCLE 2: Second measurement with SAME state (usable)
	// ==========================================
	// This is where the 0d2acf9 bug would trigger:
	// If bug exists, prev is matched against the newly inserted measurement, resetting
	// prevState to unknown and creating a second spurious state_change_event!
	sched.measureOne(ctx, *corridor)

	mList2, err := st.ListMeasurements(ctx, corridorID, 10, 0)
	if err != nil || len(mList2) != 2 {
		t.Fatalf("cycle 2: expected 2 measurements in DB, got %d (err: %v)", len(mList2), err)
	}
	if mList2[0].IntegrityState != store.StateUsable {
		t.Fatalf("cycle 2: expected StateUsable for newest measurement, got %s", mList2[0].IntegrityState)
	}

	// REGRESSION ASSERTION:
	// Event count must STILL be 1 because the state did not change!
	events2, err := getCorridorEvents(ctx, st, corridorID)
	if err != nil {
		t.Fatalf("cycle 2: error fetching events: %v", err)
	}
	if len(events2) != 1 {
		t.Fatalf("REGRESSION BUG DETECTED (commit 0d2acf9): expected exactly 1 state change event after identical measurement cycle, got %d. Previous measurement must be fetched BEFORE inserting the new one!", len(events2))
	}
	if calls := atomic.LoadInt64(&webhookCalls); calls != 1 {
		t.Fatalf("REGRESSION BUG DETECTED: expected webhook count to remain 1, got %d (spurious webhook fired)", calls)
	}

	// ==========================================
	// CYCLE 3: Third measurement with CHANGED state (unusable)
	// ==========================================
	// Set destination amount to 80.0 (100 * 0.90 = 90 expected -> (90-80)/90 * 100 = 11.1% loss -> unusable)
	destAmount.Store("80.0000000")
	sched.measureOne(ctx, *corridor)

	mList3, err := st.ListMeasurements(ctx, corridorID, 10, 0)
	if err != nil || len(mList3) != 3 {
		t.Fatalf("cycle 3: expected 3 measurements, got %d (err: %v)", len(mList3), err)
	}
	if mList3[0].IntegrityState != store.StateUnusable {
		t.Fatalf("cycle 3: expected newest measurement to be StateUnusable, got %s", mList3[0].IntegrityState)
	}

	// State changed from usable -> unusable: exactly 1 new event recorded (total = 2)
	events3, err := getCorridorEvents(ctx, st, corridorID)
	if err != nil {
		t.Fatalf("cycle 3: error fetching events: %v", err)
	}
	if len(events3) != 2 {
		t.Fatalf("cycle 3: expected 2 total state change events, got %d", len(events3))
	}
	if events3[1].FromState != store.StateUsable || events3[1].ToState != store.StateUnusable {
		t.Errorf("cycle 3: expected transition usable -> unusable, got %s -> %s", events3[1].FromState, events3[1].ToState)
	}
	if calls := atomic.LoadInt64(&webhookCalls); calls != 2 {
		t.Errorf("cycle 3: expected 2 webhook calls, got %d", calls)
	}
}

func TestScheduler_MeasureAll_Sequential(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mockFrankfurter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"date":"2026-10-07","base":"USD","quote":"EUR","rate":0.90}]`))
	}))
	defer mockFrankfurter.Close()

	mockHorizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"_embedded": {
				"records": [{
					"source_amount": "100.0000000",
					"destination_amount": "89.5000000",
					"path": []
				}]
			}
		}`))
	}))
	defer mockHorizon.Close()

	refFetcher := reference.NewFrankfurterFetcher(mockFrankfurter.URL)
	horizonClient := measure.NewHorizonClient(mockHorizon.URL, refFetcher)
	dispatcher := webhook.NewDispatcher("", webhook.FormatRaw, logger)

	sched := New(st, horizonClient, dispatcher, logger, 100.0, 10*time.Second, 2.5, 5.0)

	// Ensure measureAll executes sequentially without errors
	sched.measureAll(ctx)
}

func TestScheduler_RunContextCancel(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockHorizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"_embedded":{"records":[]}}`))
	}))
	defer mockHorizon.Close()

	horizonClient := measure.NewHorizonClient(mockHorizon.URL, nil)
	dispatcher := webhook.NewDispatcher("", webhook.FormatRaw, logger)
	sched := New(st, horizonClient, dispatcher, logger, 100.0, 100*time.Millisecond, 2.5, 5.0)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	// Allow one iteration then cancel context
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Clean exit
	case <-time.After(2 * time.Second):
		t.Fatal("Scheduler.Run did not stop within 2 seconds of context cancellation")
	}
}

// helper to query all state_change_events for a corridor in chronological order
func getCorridorEvents(ctx context.Context, st *store.Store, corridorID int) ([]store.StateChangeEvent, error) {
	return st.ListStateChangesByCorridor(ctx, corridorID)
}
