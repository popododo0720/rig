package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/storage"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Import rig.yaml and state.json into SQLite",
	Long:  "Migrates existing YAML configuration and JSON state files into the SQLite database.",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		statePath, _ := cmd.Flags().GetString("state")

		if configPath == "" {
			configPath = "rig.yaml"
		}
		if statePath == "" {
			statePath = defaultStatePath
		}

		db, err := storage.Open(defaultDBPath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer db.Close()

		var imported int

		// Import YAML config into SQLite settings.
		if _, err := os.Stat(configPath); err == nil {
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config %s: %w", configPath, err)
			}

			sections := map[string]interface{}{
				"project":  cfg.Project,
				"source":   cfg.Source,
				"ai":       cfg.AI,
				"deploy":   cfg.Deploy,
				"test":     cfg.Test,
				"workflow": cfg.Workflow,
				"notify":   cfg.Notify,
				"server":   cfg.Server,
				"projects": cfg.Projects,
			}

			for key, val := range sections {
				data, err := json.Marshal(val)
				if err != nil {
					return fmt.Errorf("marshal %s: %w", key, err)
				}
				if err := db.SetSetting(key, string(data)); err != nil {
					return fmt.Errorf("save setting %s: %w", key, err)
				}
			}

			imported++
			log.Printf("Imported config from %s", configPath)
		} else {
			log.Printf("No config file found at %s, skipping", configPath)
		}

		// Import state.json tasks into SQLite.
		if _, err := os.Stat(statePath); err == nil {
			state, err := core.LoadState(statePath)
			if err != nil {
				return fmt.Errorf("load state %s: %w", statePath, err)
			}

			for i := range state.Tasks {
				if err := db.SaveTask(&state.Tasks[i]); err != nil {
					return fmt.Errorf("save task %s: %w", state.Tasks[i].ID, err)
				}
			}

			imported++
			log.Printf("Imported %d tasks from %s", len(state.Tasks), statePath)
		} else {
			log.Printf("No state file found at %s, skipping", statePath)
		}

		if imported == 0 {
			fmt.Println("Nothing to import. Provide --config or --state paths.")
		} else {
			fmt.Printf("Migration complete. Database: %s\n", defaultDBPath())
		}

		return nil
	},
}
