package main

import (
	"fmt"
	"os"

	"github.com/rigdev/rig/internal/config"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "rig",
	Short: "Rig — AI Dev Agent Orchestrator",
	Long:  "Rig automates the full development cycle: issue → code → deploy → test → self-fix → PR",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("rig version %s\n", version)
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		if configPath == "" {
			return fmt.Errorf("--config flag is required")
		}

		_, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config validation failed: %v\n", err)
			return err
		}

		fmt.Printf("Config validation passed: %s\n", configPath)
		return nil
	},
}

func main() {
	// Register flags.
	validateCmd.Flags().StringP("config", "c", "", "Path to config file")
	_ = validateCmd.MarkFlagRequired("config")

	execCmd.Flags().StringP("config", "c", "", "Path to config file")
	execCmd.Flags().Bool("dry-run", false, "Dry-run mode (no real execution)")
	execCmd.Flags().String("step", "", "Execute only a specific step (code|deploy|test)")

	runCmd.Flags().StringP("config", "c", "", "Path to config file")
	runCmd.Flags().IntP("port", "p", 0, "Override server port")

	webCmd.Flags().StringP("config", "c", "", "Path to config file")
	webCmd.Flags().Int("port", 3000, "Dashboard server port")

	serveCmd.Flags().StringP("config", "c", "", "Path to config file")
	serveCmd.Flags().Int("web-port", 3000, "Dashboard server port")
	serveCmd.Flags().Int("webhook-port", 0, "Webhook server port (default: from config or 8080)")

	approveCmd.Flags().StringP("config", "c", "rig.yaml", "Path to config file")
	rejectCmd.Flags().StringP("config", "c", "rig.yaml", "Path to config file")

	initCmd.Flags().String("template", "custom", "Template type (custom|docker)")

	logsCmd.Flags().BoolP("follow", "f", false, "Follow logs in real-time (polls every 2s)")
	explainCmd.Flags().Bool("ai", false, "Use configured AI provider to analyze failure output")
	explainCmd.Flags().StringP("config", "c", "rig.yaml", "Path to config file (used with --ai)")

	migrateCmd.Flags().StringP("config", "c", "", "Path to config file (default: rig.yaml)")
	migrateCmd.Flags().String("state", "", "Path to state file (default: .rig/state.json)")

	// Register all commands.
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(explainCmd)
	rootCmd.AddCommand(proposalsCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(webCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
