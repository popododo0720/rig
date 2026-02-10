package deploy

import "github.com/rigdev/rig/internal/core"

// Compatibility alias for the canonical core deploy result type.
type Result = core.AdapterDeployResult

// Status holds the current deployment status.
type Status struct {
	Running bool
	URL     string
	Health  string
}
