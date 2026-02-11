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
		// Fallback: treat raw text as the plan summary.
		return &core.AIPlan{
			Summary: strings.TrimSpace(raw),
			Steps:   []string{"Implement based on AI analysis"},
		}, nil
	}

	if plan.Summary == "" {
		return nil, fmt.Errorf("parsed plan has empty summary")
	}

	return &plan, nil
}

// parseFileChanges extracts a FileChange slice from a JSON string.
// Falls back to extracting code blocks from markdown if JSON parsing fails.
func parseFileChanges(raw string) ([]core.AIFileChange, error) {
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return nil, fmt.Errorf("empty file changes response")
	}

	var changes []core.AIFileChange
	if err := json.Unmarshal([]byte(cleaned), &changes); err != nil {
		// Fallback 1: try repairing truncated JSON.
		repaired := repairTruncatedJSON(cleaned)
		if err2 := json.Unmarshal([]byte(repaired), &changes); err2 != nil {
			// Fallback 2: try to extract file changes from markdown code blocks.
			changes = extractFromMarkdown(raw)
			if len(changes) == 0 {
				return nil, fmt.Errorf("parse file changes: %w (raw: %.200s)", err, raw)
			}
		}
	}

	for i, c := range changes {
		if c.Path == "" {
			return nil, fmt.Errorf("file change %d: missing path", i)
		}
		if c.Action == "" {
			changes[i].Action = "create"
		}
	}

	return changes, nil
}

// extractFromMarkdown parses markdown with code blocks into file changes.
// Looks for patterns like: ### filename.go \n ```go \n content \n ```
func extractFromMarkdown(raw string) []core.AIFileChange {
	var changes []core.AIFileChange
	lines := strings.Split(raw, "\n")

	var currentPath string
	var currentContent strings.Builder
	inBlock := false

	for _, line := range lines {
		if inBlock {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				if currentPath != "" && currentContent.Len() > 0 {
					changes = append(changes, core.AIFileChange{
						Path:    currentPath,
						Content: strings.TrimSpace(currentContent.String()),
						Action:  "create",
					})
				}
				inBlock = false
				currentPath = ""
				currentContent.Reset()
				continue
			}
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inBlock = true
			continue
		}

		// Try to extract filename from headers or path mentions.
		path := extractPathFromLine(line)
		if path != "" {
			currentPath = path
		}
	}

	return changes
}

// extractPathFromLine tries to extract a file path from a markdown line.
func extractPathFromLine(line string) string {
	line = strings.TrimSpace(line)
	// Remove markdown headers.
	for strings.HasPrefix(line, "#") {
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
	}
	// Remove numbering like "1. " or "- ".
	for _, prefix := range []string{"1.", "2.", "3.", "4.", "5.", "6.", "7.", "8.", "9.", "-", "*"} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimPrefix(line, prefix)
			line = strings.TrimSpace(line)
		}
	}
	// Remove action words.
	for _, word := range []string{"Create ", "Modify ", "Update ", "Edit ", "create ", "modify ", "update ", "edit "} {
		line = strings.TrimPrefix(line, word)
	}
	// Extract backtick-quoted paths.
	if idx := strings.Index(line, "`"); idx >= 0 {
		end := strings.Index(line[idx+1:], "`")
		if end > 0 {
			candidate := line[idx+1 : idx+1+end]
			if looksLikePath(candidate) {
				return candidate
			}
		}
	}
	// Check if the remaining text looks like a file path.
	line = strings.TrimSpace(line)
	if looksLikePath(line) {
		return line
	}
	return ""
}

func looksLikePath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, " ") {
		return false
	}
	return strings.Contains(s, "/") || strings.Contains(s, ".")
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

// cleanJSON strips optional markdown code fences and extracts JSON from mixed text.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	// Strip ALL markdown fences aggressively.
	// Handle both multi-line (```json\n...\n```) and single-line (```json [...] ```) cases.
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			// Multi-line: strip opening fence line.
			s = s[idx+1:]
		} else {
			// Single-line: strip ```json or ``` prefix.
			s = strings.TrimPrefix(s, "```json")
			s = strings.TrimPrefix(s, "```")
		}
		// Strip closing fence.
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	// If it already starts with { or [, return it.
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		return s
	}

	// Try to extract JSON object or array from surrounding text.
	// Find the earliest opening bracket/brace.
	arrIdx := strings.IndexByte(s, '[')
	objIdx := strings.IndexByte(s, '{')

	// Pick the one that appears first.
	type candidate struct {
		start int
		close byte
	}
	var best *candidate
	if arrIdx >= 0 && (objIdx < 0 || arrIdx <= objIdx) {
		best = &candidate{start: arrIdx, close: ']'}
	} else if objIdx >= 0 {
		best = &candidate{start: objIdx, close: '}'}
	}

	if best != nil {
		end := strings.LastIndexByte(s, best.close)
		if end > best.start {
			return s[best.start : end+1]
		}
		// No closing bracket found — try to repair truncated JSON.
		partial := s[best.start:]
		repaired := repairTruncatedJSON(partial)
		if json.Valid([]byte(repaired)) {
			return repaired
		}
		return partial
	}

	return s
}

// repairTruncatedJSON tries to close unclosed brackets/braces in truncated JSON.
func repairTruncatedJSON(s string) string {
	// Count open brackets/braces that need closing.
	var stack []byte
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ']', '}':
			if len(stack) > 0 && stack[len(stack)-1] == c {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 {
		return s
	}

	// If we're inside a string (truncated mid-value), close the string first.
	suffix := ""
	if inString {
		suffix += `"`
	}
	// Close brackets in reverse order.
	for i := len(stack) - 1; i >= 0; i-- {
		suffix += string(stack[i])
	}
	return s + suffix
}
