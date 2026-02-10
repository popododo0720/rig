package main

import (
	"fmt"
	"regexp"
	"strconv"

	adapterai "github.com/rigdev/rig/internal/adapter/ai"
	adapterdeploy "github.com/rigdev/rig/internal/adapter/deploy"
	adaptergit "github.com/rigdev/rig/internal/adapter/git"
	adapternotify "github.com/rigdev/rig/internal/adapter/notify"
	adaptertest "github.com/rigdev/rig/internal/adapter/test"
	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/spf13/cobra"
)

const defaultStatePath = ".rig/state.json"

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

		issueNumber, err := strconv.Atoi(issue.ID)
		if err != nil {
			return fmt.Errorf("invalid issue number: %w", err)
		}

		engine, err := buildEngineForIssue(cfg, defaultStatePath, issueNumber)
		if err != nil {
			return err
		}
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

func buildEngine(cfg *config.Config, statePath string) (*core.Engine, error) {
	return buildEngineForIssue(cfg, statePath, 0)
}

func buildEngineForIssue(cfg *config.Config, statePath string, issueNumber int) (*core.Engine, error) {
	owner, repo, err := splitRepo(cfg.Source.Repo)
	if err != nil {
		return nil, err
	}

	gitAdapter, err := adaptergit.NewGitHub(owner, repo, cfg.Source.Token, cfg.Server.Secret, "")
	if err != nil {
		return nil, fmt.Errorf("create git adapter: %w", err)
	}

	aiAdapter, err := adapterai.NewAnthropic(cfg.AI)
	if err != nil {
		return nil, fmt.Errorf("create ai adapter: %w", err)
	}

	deployAdapter, err := adapterdeploy.NewCustom(cfg.Deploy.Config, cfg.Deploy.Rollback.Config)
	if err != nil {
		return nil, fmt.Errorf("create deploy adapter: %w", err)
	}
	if err := deployAdapter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid deploy adapter config: %w", err)
	}

	testRunners := make([]core.TestRunnerIface, 0, len(cfg.Test))
	for _, testCfg := range cfg.Test {
		if testCfg.Type != "" && testCfg.Type != "command" {
			continue
		}
		testRunners = append(testRunners, adaptertest.NewCommandRunner(testCfg))
	}

	notifiers := make([]core.NotifierIface, 0, len(cfg.Notify))
	for _, notifyCfg := range cfg.Notify {
		if notifyCfg.Type != "comment" {
			continue
		}
		if issueNumber <= 0 {
			continue
		}
		notifiers = append(notifiers, adapternotify.NewCommentNotifier(gitAdapter, owner, repo, issueNumber))
	}

	return core.NewEngine(cfg, gitAdapter, aiAdapter, deployAdapter, testRunners, notifiers, statePath), nil
}

func splitRepo(repo string) (string, string, error) {
	re := regexp.MustCompile(`^([^/]+)/([^/]+)$`)
	matches := re.FindStringSubmatch(repo)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid source.repo %q: expected owner/repo", repo)
	}
	return matches[1], matches[2], nil
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
