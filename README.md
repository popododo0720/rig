# Rig — AI Dev Agent Orchestrator

**Rig** is an AI-powered development agent that automates the full software delivery cycle: from GitHub issue to pull request. It analyzes issues, generates code, deploys changes, runs tests, and creates PRs—all autonomously with self-healing retry logic.

## Key Features

- **🤖 Autonomous Issue-to-PR Workflow**: Automatically processes GitHub issues labeled with `rig` and creates pull requests with working code
- **🔄 Self-Healing AI**: Analyzes test failures and automatically retries with fixes (configurable max retry)
- **🚀 Flexible Deployment**: Supports custom commands, Docker Compose, SSH, and more
- **🧪 Integrated Testing**: Runs tests after deployment and uses results to improve code quality
- **📊 State Machine Architecture**: Robust 10-phase execution cycle with rollback support
- **🔌 Webhook Server**: Listens for GitHub webhooks to trigger workflows automatically
- **🛠️ CLI Tools**: Validate configs, check status, view logs, and run diagnostics

## Quick Start

Get Rig running in 5 minutes:

### 1. Install

```bash
# Download binary (replace with your platform)
curl -L https://github.com/rigdev/rig/releases/latest/download/rig-linux-amd64 -o rig
chmod +x rig
sudo mv rig /usr/local/bin/

# Or build from source
git clone https://github.com/rigdev/rig.git
cd rig
make build
sudo make install
```

### 2. Initialize Configuration

```bash
# Create rig.yaml from template
rig init --template custom

# Or use Docker template
rig init --template docker
```

### 3. Configure Environment Variables

```bash
# Set required environment variables
export GITHUB_TOKEN="ghp_your_github_token"
export ANTHROPIC_API_KEY="sk-ant-your_api_key"
export WEBHOOK_SECRET="your_webhook_secret"
```

### 4. Validate Configuration

```bash
# Check configuration is valid
rig validate

# Run system diagnostics
rig doctor
```

### 5. Start Webhook Server

```bash
# Start server to listen for GitHub webhooks
rig run

# Or execute a specific issue manually
rig exec --issue 123
```

### 6. Configure GitHub Webhook

1. Go to your repository settings → Webhooks → Add webhook
2. Set Payload URL: `http://your-server:8080/webhook`
3. Set Content type: `application/json`
4. Set Secret: (same as `WEBHOOK_SECRET` env var)
5. Select events: `Issues`
6. Save webhook

### 7. Create an Issue

Create a GitHub issue with the `rig` label:

```
Title: Add health check endpoint

Body:
Add a /health endpoint that returns 200 OK with JSON status.
```

Rig will automatically:
1. Analyze the issue
2. Generate code changes
3. Create a branch and commit
4. Deploy the changes
5. Run tests
6. Create a pull request

## Installation

### Binary Releases

Download pre-built binaries from the [releases page](https://github.com/rigdev/rig/releases):

```bash
# Linux
curl -L https://github.com/rigdev/rig/releases/latest/download/rig-linux-amd64 -o rig
chmod +x rig
sudo mv rig /usr/local/bin/

# macOS
curl -L https://github.com/rigdev/rig/releases/latest/download/rig-darwin-amd64 -o rig
chmod +x rig
sudo mv rig /usr/local/bin/

# Windows
# Download rig-windows-amd64.exe and add to PATH
```

### Build from Source

```bash
git clone https://github.com/rigdev/rig.git
cd rig
make build
sudo make install
```

### Docker

```bash
docker pull rigdev/rig:latest

# Run with config mounted
docker run -v $(pwd)/rig.yaml:/rig.yaml rigdev/rig:latest validate
```

## Configuration Guide

Rig uses a `rig.yaml` configuration file. Generate a template with `rig init`.

### Configuration Structure

```yaml
# Project metadata
project:
  name: my-web-app
  language: go
  description: "Production web application"

# Source code repository
source:
  platform: github            # github | gitlab | bitbucket | gitea
  repo: owner/repo            # owner/repo format
  base_branch: main           # branch to open PRs against
  token: ${GITHUB_TOKEN}      # GitHub personal access token

# AI provider
ai:
  provider: anthropic                    # anthropic | openai | ollama
  model: claude-sonnet-4-20250514        # model identifier
  api_key: ${ANTHROPIC_API_KEY}          # API key
  max_retry: 3                           # max self-fix attempts (1–10)
  context:                               # project-specific context
    - "Go 1.22 web application"
    - "PostgreSQL database"
    - "REST API follows JSON:API spec"

# Deployment configuration
deploy:
  method: custom                         # custom | docker-compose | terraform | ansible | k8s
  config:
    commands:
      - name: build
        run: "go build -o bin/server ./cmd/server"
        workdir: "."
        timeout: 120s
        transport:
          type: local                    # local | ssh
  timeout: 600s
  rollback:
    enabled: true
    method: custom
    config:
      commands:
        - name: rollback
          run: "echo 'Rolling back...'"

# Tests
test:
  - type: command
    name: unit-tests
    run: "go test ./..."
    timeout: 120s

# Workflow triggers
workflow:
  trigger:
    - event: issue.opened
      labels: ["rig"]                    # only process issues with these labels
  steps: ["code", "deploy", "test", "report"]
  approval:
    before_deploy: false                 # set true for production safety

# Notifications
notify:
  - type: comment                        # post status as GitHub issue comment
    on: ["all"]                          # deploy | test_fail | test_pass | pr_created | all

# Webhook server
server:
  port: 8080
  secret: ${WEBHOOK_SECRET}              # GitHub webhook secret
```

### Environment Variables

Rig resolves environment variables using `${VAR_NAME}` syntax:

```yaml
source:
  token: ${GITHUB_TOKEN}

ai:
  api_key: ${ANTHROPIC_API_KEY}

server:
  secret: ${WEBHOOK_SECRET}
```

Set these in your shell or `.env` file:

```bash
export GITHUB_TOKEN="ghp_your_token"
export ANTHROPIC_API_KEY="sk-ant-your_key"
export WEBHOOK_SECRET="your_secret"
```

### Deployment Methods

#### Custom Commands

Run arbitrary commands locally or via SSH:

```yaml
deploy:
  method: custom
  config:
    commands:
      - name: build
        run: "make build"
        workdir: "."
        timeout: 120s
        transport:
          type: local
      - name: deploy-staging
        run: "systemctl restart my-app"
        workdir: "/opt/my-app"
        timeout: 300s
        transport:
          type: ssh
          host: staging.example.com
          port: 22
          user: deploy
          key_path: ~/.ssh/deploy_key
```

#### Docker Compose

Deploy using docker-compose:

```yaml
deploy:
  method: docker-compose
  config:
    file: docker-compose.yml
    env_file: .env
    services: ["app", "db"]
```

## CLI Command Reference

### `rig init`

Generate a `rig.yaml` configuration template.

```bash
# Create custom template (default)
rig init

# Create Docker template
rig init --template docker
```

**Options:**
- `--template <name>`: Template to use (`custom` or `docker`)

### `rig validate`

Validate the `rig.yaml` configuration file.

```bash
rig validate

# Validate specific file
rig validate --config /path/to/rig.yaml
```

**Options:**
- `--config <path>`: Path to config file (default: `./rig.yaml`)

### `rig exec`

Execute workflow for a specific GitHub issue.

```bash
# Execute issue #123
rig exec --issue 123

# Dry-run mode (no state mutation)
rig exec --issue 123 --dry-run
```

**Options:**
- `--issue <number>`: GitHub issue number (required)
- `--dry-run`: Dry-run mode (no state mutation)
- `--config <path>`: Path to config file (default: `./rig.yaml`)

### `rig run`

Start the webhook server to listen for GitHub events.

```bash
# Start server on port 8080 (from config)
rig run

# Override port
rig run --port 9000
```

**Options:**
- `--port <number>`: Override server port from config
- `--config <path>`: Path to config file (default: `./rig.yaml`)

### `rig status`

Show status of all tasks.

```bash
# Show all tasks
rig status

# Show specific task
rig status --task task-123
```

**Options:**
- `--task <id>`: Show specific task ID
- `--state <path>`: Path to state file (default: `./.rig-state.json`)

### `rig logs`

Show logs for a specific task.

```bash
# Show logs for task-123
rig logs --task task-123

# Follow logs (tail -f style)
rig logs --task task-123 --follow
```

**Options:**
- `--task <id>`: Task ID (required)
- `--follow`: Follow logs in real-time
- `--state <path>`: Path to state file (default: `./.rig-state.json`)

### `rig doctor`

Run system diagnostics to check configuration and dependencies.

```bash
rig doctor
```

Checks:
- Configuration file validity
- Environment variables
- GitHub API connectivity
- AI provider API connectivity
- Deployment method availability
- Test runner availability

## Architecture

Rig uses a **state machine architecture** with adapters for extensibility.

### 10-Phase Execution Cycle

```
┌─────────────┐
│   queued    │  Issue received, task created
└──────┬──────┘
       │
┌──────▼──────┐
│  planning   │  AI analyzes issue, creates plan
└──────┬──────┘
       │
┌──────▼──────┐
│   coding    │  AI generates code changes
└──────┬──────┘
       │
┌──────▼──────┐
│ committing  │  Git creates branch, commits, pushes
└──────┬──────┘
       │
┌──────▼──────┐
│ deploying   │  Deploy adapter executes deployment
└──────┬──────┘
       │
┌──────▼──────┐
│  testing    │  Test runners execute tests
└──────┬──────┘
       │
       ├─ PASS ──┐
       │         │
       │    ┌────▼────┐
       │    │reporting│  Create pull request
       │    └────┬────┘
       │         │
       │    ┌────▼────┐
       │    │completed│  Success!
       │    └─────────┘
       │
       └─ FAIL ──┐
                 │
            ┌────▼────┐
            │  retry  │  AI analyzes failure, generates fix
            └────┬────┘
                 │
                 ├─ Max retry not exceeded → back to coding
                 │
                 └─ Max retry exceeded ──┐
                                         │
                                    ┌────▼────┐
                                    │rollback │  Rollback deployment
                                    └────┬────┘
                                         │
                                    ┌────▼────┐
                                    │ failed  │  Task failed
                                    └─────────┘
```

### Adapter Architecture

Rig uses **adapters** to decouple core logic from external systems:

- **GitAdapter**: GitHub, GitLab, Bitbucket, Gitea
- **AIAdapter**: Anthropic Claude, OpenAI, Ollama
- **DeployAdapter**: Custom commands, Docker Compose, Terraform, Ansible, Kubernetes
- **TestRunner**: Command-based tests
- **Notifier**: GitHub comments, Slack, email

### Core Components

```
cmd/rig/              # CLI entry point
  ├── main.go         # Main CLI
  ├── init.go         # rig init command
  ├── validate.go     # rig validate command
  ├── exec.go         # rig exec command
  ├── run.go          # rig run command
  ├── status.go       # rig status command
  ├── logs.go         # rig logs command
  └── doctor.go       # rig doctor command

internal/
  ├── config/         # Configuration loading and validation
  ├── core/           # Core engine and state machine
  │   ├── engine.go   # Main execution engine
  │   ├── state.go    # State machine and persistence
  │   └── steps.go    # Individual execution steps
  ├── adapter/        # Adapter implementations
  │   ├── git/        # Git adapters (GitHub, GitLab, etc.)
  │   ├── ai/         # AI adapters (Anthropic, OpenAI, etc.)
  │   ├── deploy/     # Deploy adapters (custom, docker-compose, etc.)
  │   ├── test/       # Test runners
  │   └── notify/     # Notifiers
  └── server/         # Webhook server
      └── webhook.go  # GitHub webhook handler
```

## Example Workflow

### 1. Create GitHub Issue

```
Title: Add user authentication endpoint

Body:
Add a POST /api/auth/login endpoint that:
- Accepts email and password
- Returns JWT token on success
- Returns 401 on invalid credentials
```

### 2. Rig Processes Issue

```
[rig] Task task-abc123 → queued (issue: Add user authentication endpoint)
[rig] Task task-abc123 → planning (issue: Add user authentication endpoint)
[rig] Task task-abc123 → coding (issue: Add user authentication endpoint)
[rig] Task task-abc123 → committing (issue: Add user authentication endpoint)
[rig] Task task-abc123 → deploying (issue: Add user authentication endpoint)
[rig] Task task-abc123 → testing (issue: Add user authentication endpoint)
```

### 3. Tests Pass → PR Created

```
[rig] Task task-abc123 → reporting (issue: Add user authentication endpoint)
[rig] Task task-abc123 → completed (issue: Add user authentication endpoint)
[rig] Task task-abc123 completed with PR https://github.com/owner/repo/pull/42
```

### 4. Review and Merge

The PR includes:
- Code changes
- Commit message
- Test results
- Deployment logs

## Development Guide

### Prerequisites

- Go 1.25 or later
- Git
- Docker (optional, for Docker builds)

### Build

```bash
# Build binary
make build

# Run tests
make test

# Run linter
make lint

# Build Docker image
make docker-build

# Clean build artifacts
make clean
```

### Project Structure

```
rig/
├── cmd/rig/              # CLI commands
├── internal/             # Internal packages
│   ├── config/           # Configuration
│   ├── core/             # Core engine
│   ├── adapter/          # Adapters
│   └── server/           # Webhook server
├── templates/            # Init templates
│   ├── custom.yaml       # Custom deploy template
│   └── docker.yaml       # Docker deploy template
├── Dockerfile            # Multi-stage Docker build
├── Makefile              # Build automation
├── go.mod                # Go module definition
├── rig.yaml.example      # Example configuration
├── LICENSE               # MIT license
└── README.md             # This file
```

### Adding a New Adapter

1. Define the interface in `internal/core/`
2. Implement the adapter in `internal/adapter/<type>/`
3. Register the adapter in the factory
4. Add configuration schema to `internal/config/`
5. Add tests

Example: Adding a new AI provider

```go
// internal/core/ai.go
type AIAdapter interface {
    AnalyzeIssue(ctx context.Context, issue *AIIssue, projectCtx string) (*Plan, error)
    GenerateCode(ctx context.Context, plan *Plan, feedback *Feedback) ([]FileChange, error)
}

// internal/adapter/ai/openai/openai.go
type OpenAIAdapter struct {
    apiKey string
    model  string
}

func (a *OpenAIAdapter) AnalyzeIssue(ctx context.Context, issue *AIIssue, projectCtx string) (*Plan, error) {
    // Implementation
}
```

### Running Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./internal/core/...

# Run with coverage
go test -cover ./...
```

### Debugging

Enable debug logging:

```bash
export RIG_DEBUG=1
rig run
```

## Contributing

Contributions are welcome! Please follow these guidelines:

1. **Fork the repository** and create a feature branch
2. **Write tests** for new functionality
3. **Run linter** with `make lint`
4. **Update documentation** if needed
5. **Submit a pull request** with a clear description

### Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Write clear commit messages
- Add comments for exported functions

### Reporting Issues

Please include:
- Rig version (`rig --version`)
- Operating system
- Configuration file (sanitized)
- Steps to reproduce
- Expected vs actual behavior

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Support

- **Documentation**: [https://github.com/rigdev/rig](https://github.com/rigdev/rig)
- **Issues**: [https://github.com/rigdev/rig/issues](https://github.com/rigdev/rig/issues)
- **Discussions**: [https://github.com/rigdev/rig/discussions](https://github.com/rigdev/rig/discussions)

---

**Built with ❤️ by the Rig Dev team**
