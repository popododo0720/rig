package config

import (
	"strings"
	"testing"
)

func TestValidateRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "empty config",
			cfg:     Config{},
			wantErr: "project.name",
		},
		{
			name: "missing source platform",
			cfg: Config{
				Project: ProjectConfig{Name: "test"},
				AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
				Deploy:  DeployConfig{Method: "custom", Config: DeployMethodConfig{Commands: []CustomCommand{{Name: "a", Run: "b"}}}},
			},
			wantErr: "source.platform",
		},
		{
			name: "missing source repo",
			cfg: Config{
				Project: ProjectConfig{Name: "test"},
				Source:  SourceConfig{Platform: "github"},
				AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
				Deploy:  DeployConfig{Method: "custom", Config: DeployMethodConfig{Commands: []CustomCommand{{Name: "a", Run: "b"}}}},
			},
			wantErr: "source.repo",
		},
		{
			name: "missing ai provider",
			cfg: Config{
				Project: ProjectConfig{Name: "test"},
				Source:  SourceConfig{Platform: "github", Repo: "a/b"},
				AI:      AIConfig{Model: "gpt-4"},
				Deploy:  DeployConfig{Method: "custom", Config: DeployMethodConfig{Commands: []CustomCommand{{Name: "a", Run: "b"}}}},
			},
			wantErr: "ai.provider",
		},
		{
			name: "missing ai model",
			cfg: Config{
				Project: ProjectConfig{Name: "test"},
				Source:  SourceConfig{Platform: "github", Repo: "a/b"},
				AI:      AIConfig{Provider: "openai"},
				Deploy:  DeployConfig{Method: "custom", Config: DeployMethodConfig{Commands: []CustomCommand{{Name: "a", Run: "b"}}}},
			},
			wantErr: "ai.model",
		},
		{
			name: "missing deploy method",
			cfg: Config{
				Project: ProjectConfig{Name: "test"},
				Source:  SourceConfig{Platform: "github", Repo: "a/b"},
				AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
			},
			wantErr: "deploy.method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateDeployMethods(t *testing.T) {
	base := func() Config {
		return Config{
			Project: ProjectConfig{Name: "test"},
			Source:  SourceConfig{Platform: "github", Repo: "a/b"},
			AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
		}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "docker-compose missing file",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{Method: "docker-compose"}
				return c
			}(),
			wantErr: "docker-compose",
		},
		{
			name: "terraform missing dir",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{Method: "terraform"}
				return c
			}(),
			wantErr: "terraform",
		},
		{
			name: "ansible missing playbook",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{Method: "ansible"}
				return c
			}(),
			wantErr: "ansible",
		},
		{
			name: "k8s missing manifest",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{Method: "k8s"}
				return c
			}(),
			wantErr: "k8s",
		},
		{
			name: "custom no commands",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{Method: "custom"}
				return c
			}(),
			wantErr: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateCustomCommands(t *testing.T) {
	base := func() Config {
		return Config{
			Project: ProjectConfig{Name: "test"},
			Source:  SourceConfig{Platform: "github", Repo: "a/b"},
			AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
		}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "command missing name",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{Run: "echo hi"}},
					},
				}
				return c
			}(),
			wantErr: "name is required",
		},
		{
			name: "command missing run",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{Name: "build"}},
					},
				}
				return c
			}(),
			wantErr: "run is required",
		},
		{
			name: "ssh transport missing host",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{
							Name: "deploy",
							Run:  "echo deploy",
							Transport: TransportConfig{
								Type: "ssh",
								SSH:  SSHConfig{User: "root", Key: "/key"},
							},
						}},
					},
				}
				return c
			}(),
			wantErr: "host is required",
		},
		{
			name: "ssh transport missing user",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{
							Name: "deploy",
							Run:  "echo deploy",
							Transport: TransportConfig{
								Type: "ssh",
								SSH:  SSHConfig{Host: "server.com", Key: "/key"},
							},
						}},
					},
				}
				return c
			}(),
			wantErr: "user is required",
		},
		{
			name: "ssh transport missing auth",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{
							Name: "deploy",
							Run:  "echo deploy",
							Transport: TransportConfig{
								Type: "ssh",
								SSH:  SSHConfig{Host: "server.com", User: "root"},
							},
						}},
					},
				}
				return c
			}(),
			wantErr: "key or",
		},
		{
			name: "retry out of range",
			cfg: func() Config {
				c := base()
				c.Deploy = DeployConfig{
					Method: "custom",
					Config: DeployMethodConfig{
						Commands: []CustomCommand{{
							Name:  "build",
							Run:   "echo build",
							Retry: 15,
						}},
					},
				}
				return c
			}(),
			wantErr: "retry must be between 0 and 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateRollback(t *testing.T) {
	base := func() Config {
		return Config{
			Project: ProjectConfig{Name: "test"},
			Source:  SourceConfig{Platform: "github", Repo: "a/b"},
			AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
			Deploy: DeployConfig{
				Method: "custom",
				Config: DeployMethodConfig{
					Commands: []CustomCommand{{Name: "build", Run: "echo build"}},
				},
			},
		}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "rollback enabled no method",
			cfg: func() Config {
				c := base()
				c.Deploy.Rollback = RollbackConfig{Enabled: true}
				return c
			}(),
			wantErr: "rollback",
		},
		{
			name: "rollback custom no commands",
			cfg: func() Config {
				c := base()
				c.Deploy.Rollback = RollbackConfig{
					Enabled: true,
					Method:  "custom",
				}
				return c
			}(),
			wantErr: "rollback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateTests(t *testing.T) {
	base := func() Config {
		return Config{
			Project: ProjectConfig{Name: "test"},
			Source:  SourceConfig{Platform: "github", Repo: "a/b"},
			AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
			Deploy: DeployConfig{
				Method: "custom",
				Config: DeployMethodConfig{
					Commands: []CustomCommand{{Name: "build", Run: "echo build"}},
				},
			},
		}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "command test missing run",
			cfg: func() Config {
				c := base()
				c.Test = []TestConfig{{Type: "command", Name: "unit"}}
				return c
			}(),
			wantErr: "run is required",
		},
		{
			name: "ai-verify missing prompt",
			cfg: func() Config {
				c := base()
				c.Test = []TestConfig{{Type: "ai-verify", Name: "check", Tools: []string{"curl"}}}
				return c
			}(),
			wantErr: "prompt is required",
		},
		{
			name: "ai-verify missing name",
			cfg: func() Config {
				c := base()
				c.Test = []TestConfig{{Type: "ai-verify", Prompt: "check it", Tools: []string{"curl"}}}
				return c
			}(),
			wantErr: "name is required",
		},
		{
			name: "ai-verify missing tools",
			cfg: func() Config {
				c := base()
				c.Test = []TestConfig{{Type: "ai-verify", Name: "check", Prompt: "check it"}}
				return c
			}(),
			wantErr: "tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Name: "test"},
		Source:  SourceConfig{Platform: "github", Repo: "a/b"},
		AI:      AIConfig{Provider: "openai", Model: "gpt-4", MaxRetry: 3},
		Deploy: DeployConfig{
			Method: "custom",
			Config: DeployMethodConfig{
				Commands: []CustomCommand{{Name: "build", Run: "echo build"}},
			},
		},
		Test: []TestConfig{
			{Type: "command", Name: "unit", Run: "go test ./..."},
			{Type: "ai-verify", Name: "check-api", Prompt: "Verify API", Tools: []string{"curl"}},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}

func TestValidateSSHPasswordOnly(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Name: "test"},
		Source:  SourceConfig{Platform: "github", Repo: "a/b"},
		AI:      AIConfig{Provider: "openai", Model: "gpt-4", MaxRetry: 3},
		Deploy: DeployConfig{
			Method: "custom",
			Config: DeployMethodConfig{
				Commands: []CustomCommand{{
					Name: "deploy",
					Run:  "echo deploy",
					Transport: TransportConfig{
						Type: "ssh",
						SSH: SSHConfig{
							Host:     "example.com",
							User:     "deploy",
							Password: "secret",
						},
					},
				}},
			},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected password-only ssh config to validate, got: %v", err)
	}
}

func TestValidateInvalidDeployMethod(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Name: "test"},
		Source:  SourceConfig{Platform: "github", Repo: "a/b"},
		AI:      AIConfig{Provider: "openai", Model: "gpt-4"},
		Deploy:  DeployConfig{Method: "invalid-method"},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid deploy method, got nil")
	}
	if !strings.Contains(err.Error(), "deploy.method") {
		t.Errorf("error = %q, want it to contain 'deploy.method'", err.Error())
	}
}
