package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rigdev/rig/internal/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment and configuration health",
	RunE: func(cmd *cobra.Command, args []string) error {
		allOK := true

		fmt.Println("=== Rig Doctor ===")
		fmt.Println()

		// Check git.
		if checkCommand("git", "--version") {
			fmt.Println("[OK] git is installed")
		} else {
			fmt.Println("[FAIL] git is not installed or not in PATH")
			allOK = false
		}

		// Check go.
		if checkCommand("go", "version") {
			fmt.Println("[OK] go is installed")
		} else {
			fmt.Println("[FAIL] go is not installed or not in PATH")
			allOK = false
		}

		// Check config file.
		configPath := "rig.yaml"
		if _, err := os.Stat(configPath); err == nil {
			fmt.Printf("[OK] config file found: %s\n", configPath)

			// Try to validate (may fail due to env vars).
			if _, err := config.LoadConfig(configPath); err != nil {
				fmt.Printf("[WARN] config validation: %v\n", err)
			} else {
				fmt.Println("[OK] config is valid")
			}
		} else {
			fmt.Printf("[WARN] config file not found: %s (run 'rig init' to create one)\n", configPath)
		}

		// Check state directory.
		stateDir := filepath.Join(".", ".rig")
		if _, err := os.Stat(stateDir); err == nil {
			fmt.Printf("[OK] state directory exists: %s\n", stateDir)
		} else {
			fmt.Printf("[INFO] state directory not found: %s (will be created on first execution)\n", stateDir)
		}

		fmt.Println()
		if allOK {
			fmt.Println("All checks passed!")
		} else {
			fmt.Println("Some checks failed. Please fix the issues above.")
		}

		return nil
	},
}

// checkCommand checks if a command is available in PATH.
func checkCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
