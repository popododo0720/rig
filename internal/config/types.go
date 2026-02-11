package config

import "time"

// Config is the top-level configuration for Rig.
type Config struct {
	Project  ProjectConfig  `yaml:"project" json:"project"`
	Source   SourceConfig   `yaml:"source" json:"source"`
	AI       AIConfig       `yaml:"ai" json:"ai"`
	Deploy   DeployConfig   `yaml:"deploy" json:"deploy"`
	Test     []TestConfig   `yaml:"test" json:"test"`
	Workflow WorkflowConfig `yaml:"workflow" json:"workflow"`
	Notify   []NotifyConfig `yaml:"notify" json:"notify"`
	Server   ServerConfig   `yaml:"server" json:"server"`
	Projects []ProjectEntry `yaml:"projects" json:"projects"`
}

// ProjectEntry defines an additional project target for issue intake.
type ProjectEntry struct {
	Name       string `yaml:"name" json:"name"`
	Platform   string `yaml:"platform" json:"platform"`
	Repo       string `yaml:"repo" json:"repo"`
	BaseBranch string `yaml:"base_branch" json:"base_branch"`
}

// ProjectConfig holds project metadata.
type ProjectConfig struct {
	Name        string `yaml:"name" json:"name"`
	Language    string `yaml:"language" json:"language"`
	Description string `yaml:"description" json:"description"`
}

// SourceConfig holds source code repository settings.
type SourceConfig struct {
	Platform   string `yaml:"platform" json:"platform"`     // github|gitlab|bitbucket|gitea
	Repo       string `yaml:"repo" json:"repo"`
	BaseBranch string `yaml:"base_branch" json:"base_branch"`
	Token      string `yaml:"token" json:"token"`
}

// AIConfig holds AI provider settings.
type AIConfig struct {
	Provider string   `yaml:"provider" json:"provider"` // anthropic|openai|ollama|claude-code
	Model    string   `yaml:"model" json:"model"`
	APIKey   string   `yaml:"api_key" json:"api_key"`
	MaxRetry int      `yaml:"max_retry" json:"max_retry"`
	Context  []string `yaml:"context" json:"context"`
}

// DeployConfig holds deployment settings.
type DeployConfig struct {
	Method        string               `yaml:"method" json:"method"` // custom|docker-compose|terraform|ansible|k8s
	Config        DeployMethodConfig   `yaml:"config" json:"config"`
	Timeout       time.Duration        `yaml:"timeout" json:"timeout"`
	Rollback      RollbackConfig       `yaml:"rollback" json:"rollback"`
	Approval      DeployApprovalConfig `yaml:"approval" json:"approval"`
	InfraFiles    []string             `yaml:"infra_files" json:"infra_files"`
	InfraReadonly []string             `yaml:"infra_readonly" json:"infra_readonly"`
}

// DeployApprovalConfig controls whether AI-proposed infra changes require human approval.
type DeployApprovalConfig struct {
	Mode    string        `yaml:"mode" json:"mode"`       // manual (default) | suggest-only
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

// DeployMethodConfig is a union of all deploy-method-specific fields.
type DeployMethodConfig struct {
	// custom
	Commands []CustomCommand `yaml:"commands" json:"commands,omitempty"`

	// docker-compose
	File    string `yaml:"file" json:"file,omitempty"`
	EnvFile string `yaml:"env_file" json:"env_file,omitempty"`

	// terraform
	Dir       string            `yaml:"dir" json:"dir,omitempty"`
	Workspace string            `yaml:"workspace" json:"workspace,omitempty"`
	Vars      map[string]string `yaml:"vars" json:"vars,omitempty"`

	// ansible
	Playbook  string            `yaml:"playbook" json:"playbook,omitempty"`
	Inventory string            `yaml:"inventory" json:"inventory,omitempty"`
	ExtraVars map[string]string `yaml:"extra_vars" json:"extra_vars,omitempty"`

	// k8s
	Manifest  string `yaml:"manifest" json:"manifest,omitempty"`
	Namespace string `yaml:"namespace" json:"namespace,omitempty"`
	Context   string `yaml:"context" json:"context,omitempty"`
}

// CustomCommand represents a single deploy command.
type CustomCommand struct {
	Name      string            `yaml:"name" json:"name"`
	Run       string            `yaml:"run" json:"run"`
	Workdir   string            `yaml:"workdir" json:"workdir,omitempty"`
	Timeout   time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Retry     int               `yaml:"retry" json:"retry,omitempty"`
	Env       map[string]string `yaml:"env" json:"env,omitempty"`
	Transport TransportConfig   `yaml:"transport" json:"transport"`
}

// TransportConfig controls how a command is executed.
type TransportConfig struct {
	Type string    `yaml:"type" json:"type"` // local|ssh
	SSH  SSHConfig `yaml:"ssh" json:"ssh,omitempty"`
}

// SSHConfig holds SSH connection details.
type SSHConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port,omitempty"`
	User     string `yaml:"user" json:"user"`
	Key      string `yaml:"key" json:"key,omitempty"`
	Password string `yaml:"password" json:"password,omitempty"`
}

// RollbackConfig holds rollback settings.
type RollbackConfig struct {
	Enabled bool               `yaml:"enabled" json:"enabled"`
	Method  string             `yaml:"method" json:"method,omitempty"`
	Config  DeployMethodConfig `yaml:"config" json:"config,omitempty"`
}

// TestConfig holds a single test definition.
type TestConfig struct {
	Type    string        `yaml:"type" json:"type"` // command|ai-verify
	Name    string        `yaml:"name" json:"name"`
	Run     string        `yaml:"run" json:"run,omitempty"`
	Prompt  string        `yaml:"prompt" json:"prompt,omitempty"`
	URL     string        `yaml:"url" json:"url,omitempty"`
	Tools   []string      `yaml:"tools" json:"tools,omitempty"`
	Timeout time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

// WorkflowConfig holds workflow orchestration settings.
type WorkflowConfig struct {
	Trigger  []TriggerConfig `yaml:"trigger" json:"trigger"`
	Steps    []string        `yaml:"steps" json:"steps"`
	Approval ApprovalConfig  `yaml:"approval" json:"approval"`
}

// TriggerConfig holds a single workflow trigger.
type TriggerConfig struct {
	Event   string   `yaml:"event" json:"event"`
	Labels  []string `yaml:"labels" json:"labels,omitempty"`
	Keyword string   `yaml:"keyword" json:"keyword,omitempty"`
}

// ApprovalConfig holds deployment approval settings.
type ApprovalConfig struct {
	BeforeDeploy bool          `yaml:"before_deploy" json:"before_deploy"`
	Method       string        `yaml:"method" json:"method,omitempty"`
	Approvers    []string      `yaml:"approvers" json:"approvers,omitempty"`
	Timeout      time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

// NotifyConfig holds a single notification channel.
type NotifyConfig struct {
	Type    string   `yaml:"type" json:"type"`     // slack|discord|comment
	Webhook string   `yaml:"webhook" json:"webhook,omitempty"`
	On      []string `yaml:"on" json:"on"`         // deploy|test_fail|test_pass|pr_created|all
}

// ServerConfig holds webhook server settings.
type ServerConfig struct {
	Port   int    `yaml:"port" json:"port"`
	Secret string `yaml:"secret" json:"secret"`
}
