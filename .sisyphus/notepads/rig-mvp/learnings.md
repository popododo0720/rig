# Learnings

## Task 4: State Machine (state.go)
- Go module: `github.com/rigdev/rig`, go 1.25.6
- Package `core` already existed with empty `engine.go`
- `time.Duration` JSON-serializes as nanoseconds (int64), which matches the example state JSON
- `time.Time` truncation to seconds needed for JSON round-trip equality testing (sub-second precision can differ)
- gopls not available on Windows; rely on `go build` + `go vet` for static analysis
- `PhaseFailed` needs nuanced handling: it's "inactive" for in-flight detection (webhook dedup) but NOT fully terminal (can still transition to rollback). Solved with two separate maps: `strictlyTerminalPhases` (completed, rollback) and `inactivePhases` (completed, failed, rollback).
