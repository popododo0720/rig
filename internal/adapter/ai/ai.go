package ai

import "context"

// AIAdapter defines the interface for AI-assisted code generation.
type AIAdapter interface {
	// AnalyzeIssue reads an issue and produces a plan of action.
	AnalyzeIssue(ctx context.Context, issue *Issue, projectContext string) (*Plan, error)

	// GenerateCode produces file changes from a plan and existing repository files.
	GenerateCode(ctx context.Context, plan *Plan, repoFiles map[string]string) ([]FileChange, error)

	// AnalyzeFailure reads test/build logs and suggests fixes.
	AnalyzeFailure(ctx context.Context, logs string, currentCode map[string]string) ([]FileChange, error)
}

// Issue represents a minimal issue for AI analysis.
type Issue struct {
	Title string
	Body  string
	URL   string
}

// Plan holds the AI-generated plan for resolving an issue.
type Plan struct {
	Summary string
	Steps   []string
}

// FileChange represents a single file modification.
type FileChange struct {
	Path    string // relative file path
	Content string // new file content
	Action  string // "create", "modify", or "delete"
}
