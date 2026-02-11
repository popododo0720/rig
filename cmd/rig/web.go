package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/rigdev/rig/internal/storage"
	"github.com/rigdev/rig/internal/web"
	"github.com/spf13/cobra"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		port, _ := cmd.Flags().GetInt("port")

		// Open SQLite database for settings/agents APIs.
		db, err := storage.Open(defaultDBPath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer db.Close()

		// Load config: SQLite settings → rig.yaml → setup mode.
		cfg, err := loadConfigFromSources(db, configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		handler := web.NewHandler(defaultStatePath, cfg, db)

		srv := &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		errCh := make(chan error, 1)
		go func() {
			log.Printf("Dashboard running at http://localhost:%d", port)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
			close(errCh)
		}()

		select {
		case <-ctx.Done():
			log.Println("Shutting down dashboard server...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("server shutdown: %w", err)
			}
			return nil
		case err := <-errCh:
			return err
		}
	},
}
