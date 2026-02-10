package main

import (
	"fmt"

	"github.com/rigdev/rig/internal/config"
	"github.com/spf13/cobra"
)

var rejectCmd = &cobra.Command{
	Use:   "reject <task-id>",
	Short: "Reject pending proposal and fail the task",
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

		if err := engine.Resume(cmd.Context(), taskID, false); err != nil {
			return fmt.Errorf("reject task: %w", err)
		}

		fmt.Println("Proposal rejected. Task marked as failed.")
		return nil
	},
}
