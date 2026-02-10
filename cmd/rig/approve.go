package main

import (
	"fmt"

	"github.com/rigdev/rig/internal/config"
	"github.com/spf13/cobra"
)

var approveCmd = &cobra.Command{
	Use:   "approve <task-id>",
	Short: "Approve pending proposal and resume execution",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		configPath, _ := cmd.Flags().GetString("config")

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		engine, err := buildEngine(cfg, defaultStatePath)
		if err != nil {
			return err
		}

		if err := engine.Resume(cmd.Context(), taskID, true); err != nil {
			return fmt.Errorf("resume task: %w", err)
		}

		fmt.Println("Proposal approved. Task resumed and execution completed.")
		return nil
	},
}
