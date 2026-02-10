package variable

import (
	"os"
	"slices"
	"testing"

	"github.com/rigdev/rig/internal/config"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		want     string
	}{
		{
			name:     "basic substitution",
			template: "${BRANCH_NAME}",
			vars:     map[string]string{"BRANCH_NAME": "main"},
			want:     "main",
		},
		{
			name:     "multiple variables",
			template: "${A}-${B}",
			vars:     map[string]string{"A": "foo", "B": "bar"},
			want:     "foo-bar",
		},
		{
			name:     "mixed with prefix and suffix",
			template: "prefix-${VAR}-suffix",
			vars:     map[string]string{"VAR": "value"},
			want:     "prefix-value-suffix",
		},
		{
			name:     "unresolved variable preserved",
			template: "${UNDEFINED}",
			vars:     map[string]string{},
			want:     "${UNDEFINED}",
		},
		{
			name:     "empty value",
			template: "${EMPTY}",
			vars:     map[string]string{"EMPTY": ""},
			want:     "",
		},
		{
			name:     "no variables",
			template: "plain text",
			vars:     map[string]string{},
			want:     "plain text",
		},
		{
			name:     "multiple same variable",
			template: "${VAR} and ${VAR}",
			vars:     map[string]string{"VAR": "value"},
			want:     "value and value",
		},
		{
			name:     "env prefix with vars map",
			template: "${env:TEST_VAR}",
			vars:     map[string]string{"TEST_VAR": "from_map"},
			want:     "from_map",
		},
		{
			name:     "env prefix with os.Getenv fallback",
			template: "${env:PATH}",
			vars:     map[string]string{},
			want:     os.Getenv("PATH"),
		},
		{
			name:     "variable with underscore",
			template: "${BRANCH_NAME}",
			vars:     map[string]string{"BRANCH_NAME": "feature/test"},
			want:     "feature/test",
		},
		{
			name:     "variable with numbers",
			template: "${VAR123}",
			vars:     map[string]string{"VAR123": "value"},
			want:     "value",
		},
		{
			name:     "mixed resolved and unresolved",
			template: "${FOUND}-${MISSING}",
			vars:     map[string]string{"FOUND": "yes"},
			want:     "yes-${MISSING}",
		},
		{
			name:     "adjacent variables",
			template: "${A}${B}",
			vars:     map[string]string{"A": "hello", "B": "world"},
			want:     "helloworld",
		},
		{
			name:     "empty template",
			template: "",
			vars:     map[string]string{},
			want:     "",
		},
		{
			name:     "variable at start",
			template: "${START} text",
			vars:     map[string]string{"START": "begin"},
			want:     "begin text",
		},
		{
			name:     "variable at end",
			template: "text ${END}",
			vars:     map[string]string{"END": "finish"},
			want:     "text finish",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.template, tt.vars)
			if got != tt.want {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnresolvedVars(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		want     []string
	}{
		{
			name:     "single unresolved",
			template: "${MISSING}",
			vars:     map[string]string{},
			want:     []string{"MISSING"},
		},
		{
			name:     "multiple unresolved",
			template: "${A} ${B}",
			vars:     map[string]string{},
			want:     []string{"A", "B"},
		},
		{
			name:     "mixed resolved and unresolved",
			template: "${FOUND} ${MISSING}",
			vars:     map[string]string{"FOUND": "value"},
			want:     []string{"MISSING"},
		},
		{
			name:     "all resolved",
			template: "${A} ${B}",
			vars:     map[string]string{"A": "x", "B": "y"},
			want:     []string{},
		},
		{
			name:     "no variables",
			template: "plain text",
			vars:     map[string]string{},
			want:     []string{},
		},
		{
			name:     "duplicate unresolved",
			template: "${VAR} ${VAR}",
			vars:     map[string]string{},
			want:     []string{"VAR"},
		},
		{
			name:     "env prefix unresolved",
			template: "${env:MISSING_ENV}",
			vars:     map[string]string{},
			want:     []string{"MISSING_ENV"},
		},
		{
			name:     "env prefix resolved from vars",
			template: "${env:VAR}",
			vars:     map[string]string{"VAR": "value"},
			want:     []string{},
		},
		{
			name:     "empty template",
			template: "",
			vars:     map[string]string{},
			want:     []string{},
		},
		{
			name:     "multiple unresolved with duplicates",
			template: "${A} ${B} ${A}",
			vars:     map[string]string{},
			want:     []string{"A", "B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnresolvedVars(tt.template, tt.vars)
			// Sort both slices for comparison since order doesn't matter
			slices.Sort(got)
			slices.Sort(tt.want)
			if len(got) != len(tt.want) {
				t.Errorf("UnresolvedVars() returned %d items, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("UnresolvedVars() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestResolveAll(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		vars map[string]string
		want *config.Config
	}{
		{
			name: "resolve project config",
			cfg: &config.Config{
				Project: config.ProjectConfig{
					Name:        "${PROJECT_NAME}",
					Language:    "go",
					Description: "Project: ${PROJECT_NAME}",
				},
			},
			vars: map[string]string{"PROJECT_NAME": "MyProject"},
			want: &config.Config{
				Project: config.ProjectConfig{
					Name:        "MyProject",
					Language:    "go",
					Description: "Project: MyProject",
				},
			},
		},
		{
			name: "resolve source config",
			cfg: &config.Config{
				Source: config.SourceConfig{
					Platform:   "github",
					Repo:       "${REPO}",
					BaseBranch: "${BRANCH}",
					Token:      "secret",
				},
			},
			vars: map[string]string{"REPO": "myrepo", "BRANCH": "main"},
			want: &config.Config{
				Source: config.SourceConfig{
					Platform:   "github",
					Repo:       "myrepo",
					BaseBranch: "main",
					Token:      "secret",
				},
			},
		},
		{
			name: "resolve ai config",
			cfg: &config.Config{
				AI: config.AIConfig{
					Provider: "anthropic",
					Model:    "${MODEL}",
					APIKey:   "key",
					Context:  []string{"${CONTEXT_1}", "static"},
				},
			},
			vars: map[string]string{"MODEL": "claude-3", "CONTEXT_1": "context-value"},
			want: &config.Config{
				AI: config.AIConfig{
					Provider: "anthropic",
					Model:    "claude-3",
					APIKey:   "key",
					Context:  []string{"context-value", "static"},
				},
			},
		},
		{
			name: "resolve test config slice",
			cfg: &config.Config{
				Test: []config.TestConfig{
					{
						Type: "command",
						Name: "${TEST_NAME}",
						Run:  "go test ${ARGS}",
					},
				},
			},
			vars: map[string]string{"TEST_NAME": "unit-tests", "ARGS": "./..."},
			want: &config.Config{
				Test: []config.TestConfig{
					{
						Type: "command",
						Name: "unit-tests",
						Run:  "go test ./...",
					},
				},
			},
		},
		{
			name: "resolve deploy config with nested structs",
			cfg: &config.Config{
				Deploy: config.DeployConfig{
					Method: "custom",
					Config: config.DeployMethodConfig{
						Commands: []config.CustomCommand{
							{
								Name:    "${CMD_NAME}",
								Run:     "deploy ${ENV}",
								Workdir: "/app",
								Transport: config.TransportConfig{
									Type: "ssh",
									SSH: config.SSHConfig{
										Host: "${HOST}",
										User: "deploy",
									},
								},
							},
						},
					},
				},
			},
			vars: map[string]string{"CMD_NAME": "deploy-prod", "ENV": "production", "HOST": "prod.example.com"},
			want: &config.Config{
				Deploy: config.DeployConfig{
					Method: "custom",
					Config: config.DeployMethodConfig{
						Commands: []config.CustomCommand{
							{
								Name:    "deploy-prod",
								Run:     "deploy production",
								Workdir: "/app",
								Transport: config.TransportConfig{
									Type: "ssh",
									SSH: config.SSHConfig{
										Host: "prod.example.com",
										User: "deploy",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "preserve unresolved variables",
			cfg: &config.Config{
				Project: config.ProjectConfig{
					Name: "${UNDEFINED}",
				},
			},
			vars: map[string]string{},
			want: &config.Config{
				Project: config.ProjectConfig{
					Name: "${UNDEFINED}",
				},
			},
		},
		{
			name: "nil config",
			cfg:  nil,
			vars: map[string]string{},
			want: nil,
		},
		{
			name: "empty config",
			cfg:  &config.Config{},
			vars: map[string]string{},
			want: &config.Config{},
		},
		{
			name: "resolve notify config slice",
			cfg: &config.Config{
				Notify: []config.NotifyConfig{
					{
						Type:    "slack",
						Webhook: "${WEBHOOK_URL}",
					},
				},
			},
			vars: map[string]string{"WEBHOOK_URL": "https://hooks.slack.com/..."},
			want: &config.Config{
				Notify: []config.NotifyConfig{
					{
						Type:    "slack",
						Webhook: "https://hooks.slack.com/...",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAll(tt.cfg, tt.vars)
			if !configEqual(got, tt.want) {
				t.Errorf("ResolveAll() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// configEqual compares two Config structs for equality
func configEqual(a, b *config.Config) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Compare Project
	if a.Project != b.Project {
		return false
	}

	// Compare Source
	if a.Source != b.Source {
		return false
	}

	// Compare AI
	if a.AI.Provider != b.AI.Provider || a.AI.Model != b.AI.Model || a.AI.APIKey != b.AI.APIKey {
		return false
	}
	if len(a.AI.Context) != len(b.AI.Context) {
		return false
	}
	for i, v := range a.AI.Context {
		if v != b.AI.Context[i] {
			return false
		}
	}

	// Compare Deploy
	if !deployConfigEqual(&a.Deploy, &b.Deploy) {
		return false
	}

	// Compare Test
	if len(a.Test) != len(b.Test) {
		return false
	}
	for i, v := range a.Test {
		if !testConfigEqual(&v, &b.Test[i]) {
			return false
		}
	}

	// Compare Notify
	if len(a.Notify) != len(b.Notify) {
		return false
	}
	for i, v := range a.Notify {
		if !notifyConfigEqual(&v, &b.Notify[i]) {
			return false
		}
	}

	return true
}

// deployConfigEqual compares two DeployConfig structs
func deployConfigEqual(a, b *config.DeployConfig) bool {
	if a.Method != b.Method {
		return false
	}

	// Compare Config
	if !deployMethodConfigEqual(&a.Config, &b.Config) {
		return false
	}

	return true
}

// deployMethodConfigEqual compares two DeployMethodConfig structs
func deployMethodConfigEqual(a, b *config.DeployMethodConfig) bool {
	// Compare Commands
	if len(a.Commands) != len(b.Commands) {
		return false
	}
	for i, cmd := range a.Commands {
		if !customCommandEqual(&cmd, &b.Commands[i]) {
			return false
		}
	}

	// Compare other fields
	if a.File != b.File || a.EnvFile != b.EnvFile {
		return false
	}
	if a.Dir != b.Dir || a.Workspace != b.Workspace {
		return false
	}
	if a.Playbook != b.Playbook || a.Inventory != b.Inventory {
		return false
	}
	if a.Manifest != b.Manifest || a.Namespace != b.Namespace || a.Context != b.Context {
		return false
	}

	return true
}

// customCommandEqual compares two CustomCommand structs
func customCommandEqual(a, b *config.CustomCommand) bool {
	if a.Name != b.Name || a.Run != b.Run || a.Workdir != b.Workdir {
		return false
	}
	if a.Retry != b.Retry {
		return false
	}

	// Compare Transport
	if a.Transport.Type != b.Transport.Type {
		return false
	}
	if a.Transport.SSH.Host != b.Transport.SSH.Host || a.Transport.SSH.User != b.Transport.SSH.User {
		return false
	}
	if a.Transport.SSH.Port != b.Transport.SSH.Port || a.Transport.SSH.Key != b.Transport.SSH.Key {
		return false
	}

	return true
}

// testConfigEqual compares two TestConfig structs
func testConfigEqual(a, b *config.TestConfig) bool {
	if a.Type != b.Type || a.Name != b.Name || a.Run != b.Run {
		return false
	}
	if a.Prompt != b.Prompt || a.URL != b.URL {
		return false
	}
	if len(a.Tools) != len(b.Tools) {
		return false
	}
	for i, v := range a.Tools {
		if v != b.Tools[i] {
			return false
		}
	}
	return true
}

// notifyConfigEqual compares two NotifyConfig structs
func notifyConfigEqual(a, b *config.NotifyConfig) bool {
	if a.Type != b.Type || a.Webhook != b.Webhook {
		return false
	}
	if len(a.On) != len(b.On) {
		return false
	}
	for i, v := range a.On {
		if v != b.On[i] {
			return false
		}
	}
	return true
}
