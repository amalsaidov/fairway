// Package webhook dispatches JSON POST notifications on corridor state changes.
//
// By default it sends Fairway's own raw JSON Payload. Setting the dispatcher
// format to FormatDiscord instead encodes the event as a Discord webhook
// message (an embed with a severity colour), so a Fairway webhook can target a
// Discord channel's incoming webhook URL directly.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofairway/fairway/internal/store"
)

// Format selects how the webhook request body is encoded.
type Format string

const (
	// FormatRaw sends Fairway's native Payload JSON (the default).
	FormatRaw Format = "raw"
	// FormatDiscord sends a Discord-compatible embed message.
	FormatDiscord Format = "discord"
)

// Discord embed colours for each integrity state (decimal RGB values).
const (
	discordColorUsable   = 0x2ecc71 // green
	discordColorDegraded = 0xf39c12 // orange/yellow
	discordColorUnusable = 0xe74c3c // red
	discordColorUnknown  = 0x95a5a6 // grey
)

// Payload is the JSON body posted to the webhook URL in FormatRaw.
type Payload struct {
	Event         string    `json:"event"` // always "corridor.state_change"
	OccurredAt    time.Time `json:"occurred_at"`
	CorridorID    int       `json:"corridor_id"`
	CorridorName  string    `json:"corridor_name"`
	FromState     string    `json:"from_state"`
	ToState       string    `json:"to_state"`
	MeasurementID *int64    `json:"measurement_id,omitempty"`
}

// discordPayload is the top-level body of a Discord "execute webhook" request.
// See https://discord.com/developers/docs/resources/webhook#execute-webhook.
type discordPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Timestamp   string              `json:"timestamp"` // ISO 8601 / RFC 3339
	Fields      []discordEmbedField `json:"fields,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Dispatcher sends webhook payloads to a configured URL.
type Dispatcher struct {
	url        string
	format     Format
	httpClient *http.Client
	logger     *slog.Logger
}

// NewDispatcher creates a Dispatcher. If webhookURL is empty, Dispatch is a no-op.
// Any format other than FormatDiscord (including an empty or unrecognised value)
// is treated as FormatRaw.
func NewDispatcher(webhookURL string, format Format, logger *slog.Logger) *Dispatcher {
	if !strings.EqualFold(string(format), string(FormatDiscord)) {
		format = FormatRaw
	}
	return &Dispatcher{
		url:    webhookURL,
		format: format,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Dispatch sends the state-change event as a JSON POST.
// Returns nil if the webhook URL is not configured.
func (d *Dispatcher) Dispatch(ctx context.Context, corridor store.Corridor, event store.StateChangeEvent) error {
	if d.url == "" {
		return nil
	}

	body, err := d.encode(corridor, event)
	if err != nil {
		return fmt.Errorf("marshalling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook target returned HTTP %d", resp.StatusCode)
	}

	d.logger.Info("webhook dispatched",
		"corridor_id", corridor.ID,
		"from", event.FromState,
		"to", event.ToState,
		"format", string(d.format),
		"http_status", resp.StatusCode,
	)
	return nil
}

// encode renders the request body for the dispatcher's configured format.
func (d *Dispatcher) encode(corridor store.Corridor, event store.StateChangeEvent) ([]byte, error) {
	if d.format == FormatDiscord {
		return json.Marshal(discordMessage(corridor, event))
	}
	return json.Marshal(Payload{
		Event:         "corridor.state_change",
		OccurredAt:    event.OccurredAt,
		CorridorID:    corridor.ID,
		CorridorName:  corridor.Name,
		FromState:     string(event.FromState),
		ToState:       string(event.ToState),
		MeasurementID: event.MeasurementID,
	})
}

// discordMessage builds a Discord embed describing the corridor state change.
func discordMessage(corridor store.Corridor, event store.StateChangeEvent) discordPayload {
	return discordPayload{
		Embeds: []discordEmbed{
			{
				Title:       fmt.Sprintf("Corridor %s is now %s", corridor.Name, event.ToState),
				Description: fmt.Sprintf("Integrity state changed from **%s** to **%s**.", event.FromState, event.ToState),
				Color:       discordColor(event.ToState),
				Timestamp:   event.OccurredAt.UTC().Format(time.RFC3339),
				Fields: []discordEmbedField{
					{Name: "Corridor", Value: corridor.Name, Inline: true},
					{Name: "From", Value: string(event.FromState), Inline: true},
					{Name: "To", Value: string(event.ToState), Inline: true},
				},
			},
		},
	}
}

// discordColor maps an integrity state to its Discord embed colour.
func discordColor(state store.IntegrityState) int {
	switch state {
	case store.StateUsable:
		return discordColorUsable
	case store.StateDegraded:
		return discordColorDegraded
	case store.StateUnusable:
		return discordColorUnusable
	default:
		return discordColorUnknown
	}
}
