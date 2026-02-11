package metrics

import (
	"sort"
	"time"

	"github.com/rigdev/rig/internal/core"
)

// DORAMetrics summarizes delivery performance over the last 30 days.
type DORAMetrics struct {
	DeployFrequency   float64       `json:"deploy_frequency"`
	LeadTime          time.Duration `json:"lead_time"`
	MTTR              time.Duration `json:"mttr"`
	ChangeFailureRate float64       `json:"change_failure_rate"`
}

// CalculateDORA computes DORA metrics from tasks in the last 30 days.
func CalculateDORA(tasks []core.Task) DORAMetrics {
	now := time.Now().UTC()
	since := now.Add(-30 * 24 * time.Hour)

	windowTasks := make([]core.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.CreatedAt.After(since) {
			windowTasks = append(windowTasks, task)
		}
	}

	if len(windowTasks) == 0 {
		return DORAMetrics{}
	}

	completedCount := 0
	failedCount := 0
	var totalLeadTime time.Duration
	for _, task := range windowTasks {
		switch task.Status {
		case core.PhaseCompleted:
			completedCount++
			if task.CompletedAt != nil {
				totalLeadTime += task.CompletedAt.Sub(task.CreatedAt)
			}
		case core.PhaseFailed, core.PhaseRollback:
			failedCount++
		}
	}

	metrics := DORAMetrics{}
	metrics.DeployFrequency = float64(completedCount) / 30.0
	if completedCount > 0 {
		metrics.LeadTime = totalLeadTime / time.Duration(completedCount)
	}

	terminalCount := completedCount + failedCount
	if terminalCount > 0 {
		metrics.ChangeFailureRate = (float64(failedCount) / float64(terminalCount)) * 100.0
	}

	metrics.MTTR = calculateMTTR(windowTasks)
	return metrics
}

func calculateMTTR(tasks []core.Task) time.Duration {
	type terminalEvent struct {
		status      core.TaskPhase
		completedAt time.Time
	}

	events := make([]terminalEvent, 0, len(tasks))
	for _, task := range tasks {
		if task.CompletedAt == nil {
			continue
		}
		switch task.Status {
		case core.PhaseFailed, core.PhaseRollback, core.PhaseCompleted:
			events = append(events, terminalEvent{status: task.Status, completedAt: *task.CompletedAt})
		}
	}

	if len(events) == 0 {
		return 0
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].completedAt.Before(events[j].completedAt)
	})

	var openFailure *time.Time
	var totalRecovery time.Duration
	recoveries := 0

	for _, event := range events {
		switch event.status {
		case core.PhaseFailed, core.PhaseRollback:
			if openFailure == nil {
				failureTime := event.completedAt
				openFailure = &failureTime
			}
		case core.PhaseCompleted:
			if openFailure != nil && event.completedAt.After(*openFailure) {
				totalRecovery += event.completedAt.Sub(*openFailure)
				recoveries++
				openFailure = nil
			}
		}
	}

	if recoveries == 0 {
		return 0
	}
	return totalRecovery / time.Duration(recoveries)
}
