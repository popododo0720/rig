package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/rigdev/rig/internal/core"
)

// WebhookNotifier sends notifications to Slack/Discord webhooks.
type WebhookNotifier struct {
	notifyType string
	webhookURL string
	client     *http.Client
}

var _ core.NotifierIface = (*WebhookNotifier)(nil)

// NewWebhookNotifier creates a notifier for slack or discord webhooks.
func NewWebhookNotifier(notifyType, webhookURL string) *WebhookNotifier {
	return &WebhookNotifier{
		notifyType: notifyType,
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends a webhook notification payload.
func (w *WebhookNotifier) Notify(ctx context.Context, message string) error {
	var payload map[string]string
	switch w.notifyType {
	case "slack":
		payload = map[string]string{"text": message}
	case "discord":
		payload = map[string]string{"content": message}
	default:
		return fmt.Errorf("unsupported webhook notify type %q", w.notifyType)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("webhook returned status %s and body read failed: %w", resp.Status, readErr)
		}
		log.Printf("webhook notifier received non-2xx response: status=%s body=%q", resp.Status, string(respBody))
		return fmt.Errorf("webhook returned status %s", resp.Status)
	}

	return nil
}
