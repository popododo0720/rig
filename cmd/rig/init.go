package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a rig.yaml configuration template",
	RunE: func(cmd *cobra.Command, args []string) error {
		tmpl, _ := cmd.Flags().GetString("template")
		outPath := filepath.Join(".", "rig.yaml")

		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("rig.yaml already exists; remove it first or use a different directory")
		}

		var content string
		switch tmpl {
		case "docker":
			content = dockerTemplate()
		default:
			content = customTemplate()
		}

		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write rig.yaml: %w", err)
		}

		fmt.Printf("Created rig.yaml (template: %s)\n", tmpl)
		fmt.Println("Edit the file and set your environment variables before running 'rig validate'.")
		return nil
	},
}

func customTemplate() string {
	return `project:
  name: my-project
  language: go
  description: "My project description"

source:
  platform: github
  repo: owner/repo
  base_branch: main
  token: ${GITHUB_TOKEN}

ai:
  provider: anthropic
  model: claude-opus-4-6
  api_key: ${ANTHROPIC_API_KEY}
  max_retry: 3
  context:
    - "Project context for AI"

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
  timeout: 300s
  rollback:
    enabled: false

test:
  - type: command
    name: unit-test
    run: "make test"
    timeout: 120s

workflow:
  trigger:
    - event: issues.opened
      labels: ["rig"]
  steps: ["code", "deploy", "test", "report"]
  approval:
    before_deploy: false

notify:
  - type: comment
    on: ["all"]

server:
  port: 8080
  secret: ${WEBHOOK_SECRET}
`
}

func dockerTemplate() string {
	return `project:
  name: my-project
  language: go
  description: "My project description"

source:
  platform: github
  repo: owner/repo
  base_branch: main
  token: ${GITHUB_TOKEN}

ai:
  provider: anthropic
  model: claude-opus-4-6
  api_key: ${ANTHROPIC_API_KEY}
  max_retry: 3
  context:
    - "Project context for AI"

deploy:
  method: docker-compose
  config:
    file: docker-compose.yml
    env_file: .env
  timeout: 300s
  rollback:
    enabled: true
    method: docker-compose
    config:
      file: docker-compose.yml

test:
  - type: command
    name: unit-test
    run: "docker compose exec app make test"
    timeout: 120s

workflow:
  trigger:
    - event: issues.opened
      labels: ["rig"]
  steps: ["code", "deploy", "test", "report"]
  approval:
    before_deploy: false

notify:
  - type: comment
    on: ["all"]

server:
  port: 8080
  secret: ${WEBHOOK_SECRET}
`
}
