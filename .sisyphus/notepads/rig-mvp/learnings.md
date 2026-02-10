# Learnings

## Task 4: State Machine (state.go)
- Go module: `github.com/rigdev/rig`, go 1.25.6
- Package `core` already existed with empty `engine.go`
- `time.Duration` JSON-serializes as nanoseconds (int64), which matches the example state JSON
- `time.Time` truncation to seconds needed for JSON round-trip equality testing (sub-second precision can differ)
- gopls not available on Windows; rely on `go build` + `go vet` for static analysis
- `PhaseFailed` needs nuanced handling: it's "inactive" for in-flight detection (webhook dedup) but NOT fully terminal (can still transition to rollback). Solved with two separate maps: `strictlyTerminalPhases` (completed, rollback) and `inactivePhases` (completed, failed, rollback).

## [2026-02-10] Task 7: Anthropic AI Adapter
- AI adapter interface defined in ai.go with 3 methods: AnalyzeIssue, GenerateCode, AnalyzeFailure
- Shared types (Issue, Plan, FileChange) defined in same package — no cross-adapter imports
- Anthropic implementation uses direct net/http POST to /v1/messages
- Required headers: x-api-key, anthropic-version (2023-06-01), content-type (application/json)
- Request body: model, max_tokens, system (optional), messages array
- Response: content[] array with type+text blocks; extract first text block
- cleanJSON helper strips markdown code fences that LLMs sometimes add despite instructions
- parsePlan/parseFileChanges validate required fields (empty summary, missing path/action)
- 429 rate limit returns error immediately — no retry at adapter level (retry handled by engine)
- httptest.NewServer works well for mocking; validate headers in mock handler for extra safety
- All 19 tests pass including: success paths, nil inputs, rate limit, empty response, malformed JSON, markdown fences, missing fields, context cancellation
- git adapter git.go was just a stub (package declaration only) — no existing Issue/FileChange types to reuse
- ai.go stub existed with just package declaration from earlier task scaffolding

## [2026-02-10] Task 6: GitHub Git Adapter
- GitAdapter interface in git.go with 7 methods: ParseWebhook, GetIssue, PostComment, CreateBranch, CommitAndPush, CreatePR, CloneOrPull
- 3 structs: Issue, FileChange, PullRequest — concrete types only, no `any`/`interface{}`
- `google/go-github/v60` for GitHub REST API (issues, PRs, comments)
- `client.BaseURL` can be overridden with `*url.URL` for httptest mocking — no need for `WithEnterpriseURLs` in tests
- `github.NewClient(nil).WithAuthToken(token)` for auth; `WithEnterpriseURLs(base, base)` for GHE
- Webhook HMAC-SHA256: GitHub sends `sha256=<hex>`, verify with `crypto/hmac` + `crypto/sha256`
- `hmac.Equal()` for constant-time comparison — prevents timing attacks
- Git CLI operations: call `git` directly with args (cross-platform), NOT `sh -c` wrapper
- `exec.CommandContext` with `WaitDelay` + `Cancel` for proper cleanup (matches deploy adapter pattern)
- Workspace path: `~/.rig/workspaces/<owner>/<repo>/` via `os.UserHomeDir()`
- CommitAndPush handles create/update (WriteFile + git add) and delete (git rm -f) actions
- Test pattern: `initBareRepo` helper creates bare + clone for realistic push/pull testing
- `t.TempDir()` automatically cleaned up — no manual cleanup needed
- Git tests require `git config user.email/name` in temp repos to avoid commit failures
- 14 tests total: API mocking (GetIssue, PostComment, CreatePR), webhook parsing (7 cases incl. valid/invalid HMAC, bad JSON, missing issue), local git ops (branch, commit+push, delete, invalid action, clone/pull), constructor, timeout
- All tests pass on Windows without `-race` flag (no gcc)

## [2026-02-10] Task 8: Webhook Server + Notify
- chi router v5.2.5 added as dependency for HTTP routing
- http.MaxBytesReader for body size limit (10MB) — wraps r.Body, returns 413 on overflow
- Server exposes Router() method returning http.Handler for httptest.NewServer usage
- HMAC-SHA256 verification: GitHub sends "sha256=hex" in X-Hub-Signature-256 header
- When secret is empty, signature verification is skipped (dev mode)
- Handler uses ExecuteFunc callback instead of engine import — keeps it decoupled
- Event classification: GitHub event header + JSON action field combined (e.g., "issues.opened")
- core.State.IsInFlight() reused directly for duplicate detection — no reimplementation needed
- Label matching is case-insensitive (lowered both sides)
- Keyword search checks both issue title and comment body
- CommentNotifier wraps GitAdapter.PostComment with "[rig]" prefix
- Notifier interface is minimal: single Notify(ctx, message) method
- webhook/server.go and webhook/handler.go stubs existed (package declaration only)
- 16 webhook tests + 3 notify tests = 19 total, all pass
- chi returns 405 Method Not Allowed for GET on POST-only route (good default)
- signal.NotifyContext for graceful shutdown — blocks on ctx.Done then calls server.Shutdown

## [2026-02-10] Task 9: Orchestration Engine + CLI Integration
- **Import cycle resolution**: `core` package cannot import adapter packages (git, ai, deploy, test) because they import `core` for shared types. Solution: define adapter interfaces directly in `core/workflow.go` with core-local types (`AIAdapter`, `GitAdapter`, `DeployAdapterIface`, `TestRunnerIface`, `NotifierIface`). Adapter packages implement these interfaces without `core` needing to import them.
- Core-local types mirror adapter types: `AIIssue`, `AIPlan`, `AIFileChange`, `GitFileChange`, `GitPullRequest`, `AdapterDeployResult`. This adds some duplication but cleanly breaks the cycle.
- **Engine architecture**: `Engine` struct holds config + all adapter interfaces, constructed via `NewEngine()` with explicit dependency injection. No global state, no init() functions.
- **10-step execution cycle**: queued → planning → coding → committing → (approval skip) → deploying → testing → reporting → completed. Failures at any step → PhaseFailed. Test failures trigger retry loop.
- **Retry loop** (`retry.go`): iterates up to `config.AI.MaxRetry` times. Each iteration: collect test output → AI.AnalyzeFailure → new commit → redeploy → retest. Creates new Attempt per retry. On max retry exceeded, returns error which triggers rollback.
- **State persistence**: Engine loads/saves state via `LoadState`/`SaveState` at key points. Dry-run mode skips all state mutation and adapter calls.
- **Mock adapters in tests**: Simple structs with configurable return values. `mockTestRunner` uses a slice of results indexed by call count — enables testing first-fail-then-pass retry scenarios.
- **CommandRunner** (`internal/adapter/test/command.go`): Uses `exec.CommandContext(ctx, "sh", "-c", command)` with `cmd.Cancel = kill` and `cmd.WaitDelay = 3s` for proper cleanup on timeout. Variable resolution via `variable.Resolve()` before execution.
- **CLI commands**: 8 total (init, validate, exec, run, status, logs, doctor, version). All use cobra. Flags registered in `main()` instead of `init()` to comply with "no init() functions" guideline.
- `rig exec` uses stub adapters for now — real adapter wiring deferred to production integration.
- `rig doctor` checks: git installed, go installed, config file exists + validates, state directory exists.
- `rig init --template custom|docker` generates a full rig.yaml template.
- **Windows/Git Bash**: Binary is `rig.exe`, invoked via `./rig.exe`. `sh -c` works for command execution in Git Bash environment.
- Config loading requires env vars set (GITHUB_TOKEN, ANTHROPIC_API_KEY, WEBHOOK_SECRET) even for dry-run — this is by design in `config.LoadConfig` which validates env vars.
- All 8 engine tests + 5 command runner tests pass. `go vet ./...` clean. `go build ./cmd/rig` produces working binary.
