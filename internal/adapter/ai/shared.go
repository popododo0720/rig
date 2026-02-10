package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rigdev/rig/internal/core"
)

// buildSystemPrompt constructs the system prompt from project context.
func buildSystemPrompt(projectContext string) string {
	if projectContext == "" {
		return "You are a software engineering assistant. Analyze issues and generate implementation plans."
	}
	return fmt.Sprintf(
		"You are a software engineering assistant. Project context:\n%s",
		projectContext,
	)
}

// formatSteps formats plan steps as a numbered list.
func formatSteps(steps []string) string {
	var b strings.Builder
	for i, s := range steps {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
	}
	return b.String()
}

// parsePlan extracts a Plan from a JSON string, handling optional markdown fences.
func parsePlan(raw string) (*core.AIPlan, error) {
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return nil, fmt.Errorf("empty plan response")
	}

	var plan core.AIPlan
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return nil, fmt.Errorf("parse plan: %w (raw: %.200s)", err, raw)
	}

	if plan.Summary == "" {
		return nil, fmt.Errorf("parsed plan has empty summary")
	}

	return &plan, nil
}

// parseFileChanges extracts a FileChange slice from a JSON string.
func parseFileChanges(raw string) ([]core.AIFileChange, error) {
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return nil, fmt.Errorf("empty file changes response")
	}

	var changes []core.AIFileChange
	if err := json.Unmarshal([]byte(cleaned), &changes); err != nil {
		return nil, fmt.Errorf("parse file changes: %w (raw: %.200s)", err, raw)
	}

	for i, c := range changes {
		if c.Path == "" {
			return nil, fmt.Errorf("file change %d: missing path", i)
		}
		if c.Action == "" {
			return nil, fmt.Errorf("file change %d: missing action", i)
		}
	}

	return changes, nil
}

// parseProposedFix extracts an AIProposedFix from a JSON string.
func parseProposedFix(raw string) (*core.AIProposedFix, error) {
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return nil, fmt.Errorf("empty proposed fix response")
	}

	var fix core.AIProposedFix
	if err := json.Unmarshal([]byte(cleaned), &fix); err != nil {
		return nil, fmt.Errorf("parse proposed fix: %w (raw: %.200s)", err, raw)
	}

	if fix.Summary == "" {
		return nil, fmt.Errorf("parsed proposed fix has empty summary")
	}

	for i, change := range fix.Changes {
		if change.Path == "" {
			return nil, fmt.Errorf("proposed change %d: missing path", i)
		}
		if change.Action == "" {
			return nil, fmt.Errorf("proposed change %d: missing action", i)
		}
	}

	return &fix, nil
}

// cleanJSON strips optional markdown code fences and trims whitespace.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	return s
}
