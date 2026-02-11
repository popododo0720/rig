package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookNotifySlackPayload(t *testing.T) {
	t.Parallel()

	type reqPayload struct {
		Text string `json:"text"`
	}

	var gotMethod string
	var gotContentType string
	var got reqPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	notifier := NewWebhookNotifier("slack", srv.URL)
	err := notifier.Notify(context.Background(), "build passed")
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected method POST, got %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected content type application/json, got %s", gotContentType)
	}
	if got.Text != "build passed" {
		t.Fatalf("expected text payload, got %q", got.Text)
	}
}

func TestWebhookNotifyDiscordPayload(t *testing.T) {
	t.Parallel()

	type reqPayload struct {
		Content string `json:"content"`
	}

	var got reqPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	notifier := NewWebhookNotifier("discord", srv.URL)
	err := notifier.Notify(context.Background(), "deploy failed")
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if got.Content != "deploy failed" {
		t.Fatalf("expected content payload, got %q", got.Content)
	}
}

func TestWebhookNotifyNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid payload"))
	}))
	defer srv.Close()

	notifier := NewWebhookNotifier("slack", srv.URL)
	err := notifier.Notify(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWebhookNotifyUnsupportedType(t *testing.T) {
	t.Parallel()

	notifier := NewWebhookNotifier("comment", "https://example.com")
	err := notifier.Notify(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for unsupported type, got nil")
	}
}
