package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec <issue-url>",
	Short: "Execute the full automation cycle for an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueURL := args[0]
		configPath, _ := cmd.Flags().GetString("config")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if configPath == "" {
			configPath = "rig.yaml"
		}

		// Parse issue URL to extract metadata.
		issue, err := parseIssueURL(issueURL)
		if err != nil {
			return fmt.Errorf("invalid issue URL: %w", err)
		}

		// Load configuration.
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Build engine with stub adapters for now.
		// In production, real adapters would be wired here.
		engine := core.NewEngine(
			cfg,
			&stubGit{},
			&stubAI{},
			&stubDeploy{},
			nil, // test runners built from config
			nil, // notifiers built from config
			".rig/state.json",
		)
		engine.SetDryRun(dryRun)

		if dryRun {
			fmt.Printf("Dry-run mode: would execute issue %s (%s)\n", issue.ID, issue.Title)
		}

		if err := engine.Execute(cmd.Context(), issue); err != nil {
			return fmt.Errorf("execution failed: %w", err)
		}

		if dryRun {
			fmt.Println("Dry-run completed successfully.")
		} else {
			fmt.Println("Execution completed successfully.")
		}
		return nil
	},
}

// parseIssueURL extracts issue metadata from a GitHub issue URL.
// Supports: https://github.com/{owner}/{repo}/issues/{number}
func parseIssueURL(url string) (core.Issue, error) {
	re := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)/issues/(\d+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) != 4 {
		return core.Issue{}, fmt.Errorf("URL must match https://github.com/{owner}/{repo}/issues/{number}")
	}

	owner := matches[1]
	repo := matches[2]
	number := matches[3]

	return core.Issue{
		Platform: "github",
		Repo:     owner + "/" + repo,
		ID:       number,
		Title:    fmt.Sprintf("Issue #%s", number),
		URL:      url,
	}, nil
}

// --- Stub adapters for CLI (replaced with real ones in production) ---

type stubAI struct{}

func (s *stubAI) AnalyzeIssue(_ context.Context, issue *core.AIIssue, projectCtx string) (*core.AIPlan, error) {
	return &core.AIPlan{Summary: "stub plan for " + issue.Title, Steps: []string{"stub step"}}, nil
}
func (s *stubAI) GenerateCode(_ context.Context, plan *core.AIPlan, repoFiles map[string]string) ([]core.AIFileChange, error) {
	return []core.AIFileChange{{Path: "main.go", Content: "package main", Action: "modify"}}, nil
}
func (s *stubAI) AnalyzeFailure(_ context.Context, logs string, currentCode map[string]string) ([]core.AIFileChange, error) {
	return []core.AIFileChange{{Path: "main.go", Content: "package main // fixed", Action: "modify"}}, nil
}

type stubGit struct{}

func (s *stubGit) CreateBranch(_ context.Context, branchName string) error { return nil }
func (s *stubGit) CommitAndPush(_ context.Context, changes []core.GitFileChange, msg string) error {
	return nil
}
func (s *stubGit) CreatePR(_ context.Context, base, head, title, body string) (*core.GitPullRequest, error) {
	return &core.GitPullRequest{Number: 0, URL: "", Title: title}, nil
}
func (s *stubGit) CloneOrPull(_ context.Context, owner, repo, token string) error { return nil }

type stubDeploy struct{}

func (s *stubDeploy) Validate() error { return nil }
func (s *stubDeploy) Deploy(_ context.Context, vars map[string]string) (*core.AdapterDeployResult, error) {
	return &core.AdapterDeployResult{Success: true, Output: "stub deploy"}, nil
}
func (s *stubDeploy) Rollback(_ context.Context) error { return nil }
