# Decisions

## Task 4: State Machine
- **Two-tier terminal concept**: `strictlyTerminalPhases` (no outgoing transitions at all) vs `inactivePhases` (not in-flight for webhook dedup). `failed` is inactive but not strictly terminal since `failed→rollback` is valid.
- **Atomic write**: tmp file + rename pattern. No fsync per spec. MkdirAll for parent dir safety.
- **Task ID format**: `task-YYYYMMDD-HHMMSS-NNN` (timestamp + sequence). Simple, sortable, unique enough for sequential Phase 1.
- **Branch naming**: `rig/issue-{issueID}` convention baked into CreateTask.
- **Transition function operates on *Task pointer**: mutates in place, returns error on invalid transition. Sets CompletedAt for completed/failed/rollback transitions.
