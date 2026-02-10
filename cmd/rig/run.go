package main

import (
	"fmt"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/webhook"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the webhook daemon server",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		port, _ := cmd.Flags().GetInt("port")

		if configPath == "" {
			configPath = "rig.yaml"
		}

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if port > 0 {
			cfg.Server.Port = port
		}

		// Create webhook handler with engine execute callback.
		handler := webhook.NewHandler(
			cfg.Server.Secret,
			cfg.Workflow.Trigger,
			".rig/state.json",
			func(issue core.Issue) error {
				// In production, this would create a real engine and execute.
				fmt.Printf("Received issue: %s (%s)\n", issue.ID, issue.Title)
				return nil
			},
		)

		server := webhook.NewServer(cfg.Server, handler)

		fmt.Printf("Starting rig webhook server on port %d...\n", cfg.Server.Port)
		return server.ListenAndServe(cmd.Context())
	},
}
