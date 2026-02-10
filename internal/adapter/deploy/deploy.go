package deploy

import (
	"context"
	"time"
)

// DeployAdapter defines the interface for deploy operations.
type DeployAdapter interface {
	// Validate checks that the adapter configuration is valid.
	Validate() error

	// Deploy executes the deployment with the given variable map.
	Deploy(ctx context.Context, vars map[string]string) (*Result, error)

	// Rollback reverses a deployment.
	Rollback(ctx context.Context) error

	// Status returns the current deployment status.
	Status(ctx context.Context) (*Status, error)
}

// Result holds the outcome of a deploy or rollback operation.
type Result struct {
	Success  bool
	Output   string
	Duration time.Duration
}

// Status holds the current deployment status.
type Status struct {
	Running bool
	URL     string
	Health  string
}
