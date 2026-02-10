package config

import "time"

// Config is the top-level configuration for Rig.
type Config struct {
	Project  ProjectConfig  `yaml:"project"`
	Source   SourceConfig   `yaml:"source"`
	AI       AIConfig       `yaml:"ai"`
	Deploy   DeployConfig   `yaml:"deploy"`
	Test     []TestConfig   `yaml:"test"`
	Workflow WorkflowConfig `yaml:"workflow"`
	Notify   []NotifyConfig `yaml:"notify"`
	Server   ServerConfig   `yaml:"server"`
}

// ProjectConfig holds project metadata.
type ProjectConfig struct {
	Name        string `yaml:"name"`
	Language    string `yaml:"language"`
	Description string `yaml:"description"`
}

// SourceConfig holds source code repository settings.
type SourceConfig struct {
	Platform   string `yaml:"platform"` // github|gitlab|bitbucket|gitea
	Repo       string `yaml:"repo"`
	BaseBranch string `yaml:"base_branch"`
	Token      string `yaml:"token"`
}

// AIConfig holds AI provider settings.
type AIConfig struct {
	Provider string   `yaml:"provider"` // anthropic|openai|ollama
	Model    string   `yaml:"model"`
	APIKey   string   `yaml:"api_key"`
	MaxRetry int      `yaml:"max_retry"`
	Context  []string `yaml:"context"`
}

// DeployConfig holds deployment settings.
type DeployConfig struct {
	Method        string               `yaml:"method"` // custom|docker-compose|terraform|ansible|k8s
	Config        DeployMethodConfig   `yaml:"config"`
	Timeout       time.Duration        `yaml:"timeout"`
	Rollback      RollbackConfig       `yaml:"rollback"`
	Approval      DeployApprovalConfig `yaml:"approval"`
	InfraFiles    []string             `yaml:"infra_files"`    // glob patterns for AI-modifiable infra files
	InfraReadonly []string             `yaml:"infra_readonly"` // glob patterns AI can read but not modify
}

// DeployApprovalConfig controls whether AI-proposed infra changes require human approval.
type DeployApprovalConfig struct {
	Mode    string        `yaml:"mode"`    // manual (default) | auto | suggest-only
	Timeout time.Duration `yaml:"timeout"` // how long to wait for approval (default 24h)
}

// DeployMethodConfig is a union of all deploy-method-specific fields.
type DeployMethodConfig struct {
	// custom
	Commands []CustomCommand `yaml:"commands"`

	// docker-compose
	File    string `yaml:"file"`
	EnvFile string `yaml:"env_file"`

	// terraform
	Dir       string            `yaml:"dir"`
	Workspace string            `yaml:"workspace"`
	Vars      map[string]string `yaml:"vars"`

	// ansible
	Playbook  string            `yaml:"playbook"`
	Inventory string            `yaml:"inventory"`
	ExtraVars map[string]string `yaml:"extra_vars"`

	// k8s
	Manifest  string `yaml:"manifest"`
	Namespace string `yaml:"namespace"`
	Context   string `yaml:"context"`
}

// CustomCommand represents a single deploy command.
type CustomCommand struct {
	Name      string            `yaml:"name"`
	Run       string            `yaml:"run"`
	Workdir   string            `yaml:"workdir"`
	Timeout   time.Duration     `yaml:"timeout"`
	Retry     int               `yaml:"retry"`
	Env       map[string]string `yaml:"env"`
	Transport TransportConfig   `yaml:"transport"`
}

// TransportConfig controls how a command is executed.
type TransportConfig struct {
	Type string    `yaml:"type"` // local|ssh
	SSH  SSHConfig `yaml:"ssh"`
}

// SSHConfig holds SSH connection details.
type SSHConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Key      string `yaml:"key"`
	Password string `yaml:"password"`
}

// RollbackConfig holds rollback settings.
type RollbackConfig struct {
	Enabled bool               `yaml:"enabled"`
	Method  string             `yaml:"method"`
	Config  DeployMethodConfig `yaml:"config"`
}

// TestConfig holds a single test definition.
type TestConfig struct {
	Type    string        `yaml:"type"` // command|ai-verify
	Name    string        `yaml:"name"`
	Run     string        `yaml:"run"`
	Prompt  string        `yaml:"prompt"`
	URL     string        `yaml:"url"`
	Tools   []string      `yaml:"tools"`
	Timeout time.Duration `yaml:"timeout"`
}

// WorkflowConfig holds workflow orchestration settings.
type WorkflowConfig struct {
	Trigger  []TriggerConfig `yaml:"trigger"`
	Steps    []string        `yaml:"steps"`
	Approval ApprovalConfig  `yaml:"approval"`
}

// TriggerConfig holds a single workflow trigger.
type TriggerConfig struct {
	Event   string   `yaml:"event"`
	Labels  []string `yaml:"labels"`
	Keyword string   `yaml:"keyword"`
}

// ApprovalConfig holds deployment approval settings.
type ApprovalConfig struct {
	BeforeDeploy bool          `yaml:"before_deploy"`
	Method       string        `yaml:"method"`
	Approvers    []string      `yaml:"approvers"`
	Timeout      time.Duration `yaml:"timeout"`
}

// NotifyConfig holds a single notification channel.
type NotifyConfig struct {
	Type    string   `yaml:"type"` // slack|discord|comment
	Webhook string   `yaml:"webhook"`
	On      []string `yaml:"on"` // deploy|test_fail|test_pass|pr_created|all
}

// ServerConfig holds webhook server settings.
type ServerConfig struct {
	Port   int    `yaml:"port"`
	Secret string `yaml:"secret"`
}
