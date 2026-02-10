package test

import (
	"context"

	"github.com/rigdev/rig/internal/core"
)

// TestRunner defines the interface for running tests against a deployment.
type TestRunner interface {
	// Run executes the test with the given variable map and returns a result.
	Run(ctx context.Context, vars map[string]string) (*core.TestResult, error)
}
