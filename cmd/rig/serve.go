package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/storage"
	"github.com/rigdev/rig/internal/web"
	"github.com/rigdev/rig/internal/webhook"
	"github.com/spf13/cobra"
)

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".rig", "rig.db")
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start web dashboard + webhook server together",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		webPort, _ := cmd.Flags().GetInt("web-port")
		webhookPort, _ := cmd.Flags().GetInt("webhook-port")

		// Open SQLite database
		db, err := storage.Open(defaultDBPath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer db.Close()

		// Load config: SQLite settings → rig.yaml → setup mode
		cfg, err := loadConfigFromSources(db, configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if cfg != nil && webhookPort > 0 {
			cfg.Server.Port = webhookPort
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		errCh := make(chan error, 2)

		// --- Web Dashboard (always starts) ---
		var execFn web.ExecuteFunc
		if cfg != nil {
			execFn = func(issue core.Issue) error {
				issueNumber, _ := strconv.Atoi(issue.ID)
				engine, err := buildEngineForIssue(cfg, defaultStatePath, issueNumber)
				if err != nil {
					return err
				}
				engine.SetLogFunc(func(taskID, level, message string) {
					_ = db.AppendLog(taskID, level, message)
				})
				return engine.Execute(ctx, issue)
			}
		}
		webHandler := web.NewHandler(defaultStatePath, cfg, db, execFn)
		webSrv := &http.Server{
			Addr:         fmt.Sprintf(":%d", webPort),
			Handler:      webHandler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		go func() {
			log.Printf("Dashboard running at http://localhost:%d", webPort)
			if err := webSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("web server: %w", err)
			}
		}()

		if cfg == nil {
			// Setup mode: no config yet, only web dashboard
			fmt.Printf("\n  rig serve (setup mode)\n")
			fmt.Printf("  └─ Dashboard : http://localhost:%d\n", webPort)
			fmt.Printf("\n  No configuration found. Visit the dashboard to set up.\n\n")

			select {
			case <-ctx.Done():
				log.Println("Shutting down...")
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = webSrv.Shutdown(shutdownCtx)
				return nil
			case err := <-errCh:
				return err
			}
		}

		// --- Webhook Server (full mode) ---
		whHandler := webhook.NewHandler(
			cfg.Server.Secret,
			cfg.Workflow.Trigger,
			defaultStatePath,
			func(issue core.Issue) error {
				issueNumber, err := strconv.Atoi(issue.ID)
				if err != nil {
					return fmt.Errorf("invalid issue ID %q: %w", issue.ID, err)
				}
				engine, err := buildEngineForIssue(cfg, defaultStatePath, issueNumber)
				if err != nil {
					return err
				}
				engine.SetLogFunc(func(taskID, level, message string) {
					_ = db.AppendLog(taskID, level, message)
				})
				return engine.Execute(ctx, issue)
			},
		)
		whServer := webhook.NewServer(cfg.Server, whHandler)
		go func() {
			if err := whServer.ListenAndServe(ctx); err != nil {
				errCh <- fmt.Errorf("webhook server: %w", err)
			}
		}()

		whPort := cfg.Server.Port
		if whPort == 0 {
			whPort = 8080
		}

		fmt.Printf("\n  rig serve running\n")
		fmt.Printf("  ├─ Dashboard : http://localhost:%d\n", webPort)
		fmt.Printf("  └─ Webhook   : http://localhost:%d/webhook\n\n", whPort)

		select {
		case <-ctx.Done():
			log.Println("Shutting down...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = webSrv.Shutdown(shutdownCtx)
			return nil
		case err := <-errCh:
			return err
		}
	},
}

// loadConfigFromSources tries: SQLite settings → YAML file → nil (setup mode).
func loadConfigFromSources(db *storage.DB, configPath string) (*config.Config, error) {
	// If explicit --config flag, use YAML directly
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				// YAML exists but has errors (e.g., missing env vars) - try SQLite
				log.Printf("YAML config error: %v, checking SQLite...", err)
			} else {
				return cfg, nil
			}
		}
	}

	// Try SQLite settings
	has, err := db.HasSettings()
	if err != nil {
		return nil, fmt.Errorf("check settings: %w", err)
	}
	if has {
		settings, err := db.GetAllSettings()
		if err != nil {
			return nil, fmt.Errorf("load settings: %w", err)
		}
		cfg, err := config.FromSettings(settings)
		if err != nil {
			return nil, fmt.Errorf("parse settings: %w", err)
		}
		log.Println("Loaded config from SQLite database")
		return cfg, nil
	}

	// Try default rig.yaml (no explicit flag)
	if configPath == "" {
		configPath = "rig.yaml"
	}
	if _, err := os.Stat(configPath); err == nil {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			log.Printf("Config file found but has errors: %v", err)
			log.Println("Starting in setup mode — configure via web dashboard")
			return nil, nil
		}
		return cfg, nil
	}

	// No config anywhere → setup mode
	return nil, nil
}
