package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/core"
)

func TestGetDORAMetrics(t *testing.T) {
	now := time.Now().UTC()
	completed := now.Add(-2 * time.Hour)

	state := &core.State{
		Version: "1.0",
		Tasks: []core.Task{{
			ID:          "task-1",
			Status:      core.PhaseCompleted,
			CreatedAt:   now.Add(-4 * time.Hour),
			CompletedAt: &completed,
		}},
	}

	statePath := writeStateFile(t, state)
	handler := NewHandler(statePath, testConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/dora", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := payload["deploy_frequency"]; !ok {
		t.Fatal("expected deploy_frequency field")
	}
	if _, ok := payload["lead_time"]; !ok {
		t.Fatal("expected lead_time field")
	}
	if _, ok := payload["mttr"]; !ok {
		t.Fatal("expected mttr field")
	}
	if _, ok := payload["change_failure_rate"]; !ok {
		t.Fatal("expected change_failure_rate field")
	}
}
