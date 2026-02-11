package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/rigdev/rig/internal/core"
)

func TestCalculateDORA(t *testing.T) {
	// Use a fixed reference close to now to avoid timing flakiness.
	now := time.Now().UTC().Truncate(time.Second)

	completedAt1 := now.Add(-46 * time.Hour)
	completedAt2 := now.Add(-10 * time.Hour)
	failedAt := now.Add(-23 * time.Hour)
	oldCompleted := now.Add(-45 * 24 * time.Hour)

	tasks := []core.Task{
		{
			ID:          "task-1",
			Status:      core.PhaseCompleted,
			CreatedAt:   now.Add(-48 * time.Hour),
			CompletedAt: &completedAt1,
		},
		{
			ID:          "task-2",
			Status:      core.PhaseFailed,
			CreatedAt:   now.Add(-24 * time.Hour),
			CompletedAt: &failedAt,
		},
		{
			ID:          "task-3",
			Status:      core.PhaseCompleted,
			CreatedAt:   now.Add(-12 * time.Hour),
			CompletedAt: &completedAt2,
		},
		{
			ID:          "task-old",
			Status:      core.PhaseCompleted,
			CreatedAt:   now.Add(-50 * 24 * time.Hour),
			CompletedAt: &oldCompleted,
		},
	}

	m := CalculateDORA(tasks)

	if math.Abs(m.DeployFrequency-(2.0/30.0)) > 0.0001 {
		t.Fatalf("unexpected deploy frequency: %f", m.DeployFrequency)
	}
	if m.LeadTime != 2*time.Hour {
		t.Fatalf("unexpected lead time: %s", m.LeadTime)
	}
	if m.MTTR != 13*time.Hour {
		t.Fatalf("unexpected MTTR: %s", m.MTTR)
	}
	if math.Abs(m.ChangeFailureRate-33.3333) > 0.05 {
		t.Fatalf("unexpected change failure rate: %f", m.ChangeFailureRate)
	}
}

func TestCalculateDORA_EmptyWindow(t *testing.T) {
	now := time.Now().UTC()
	completed := now.Add(-59 * 24 * time.Hour)

	tasks := []core.Task{{
		ID:          "task-old",
		Status:      core.PhaseCompleted,
		CreatedAt:   now.Add(-60 * 24 * time.Hour),
		CompletedAt: &completed,
	}}

	m := CalculateDORA(tasks)
	if m != (DORAMetrics{}) {
		t.Fatalf("expected zero metrics, got %+v", m)
	}
}
