package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version string  `yaml:"version"`
	Groups  []Group `yaml:"groups"`
}

type Group struct {
	Name     string    `yaml:"name"`
	Services []Service `yaml:"services"`
}

type Service struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	WorkingDir  string            `yaml:"working_dir"`
	Env         map[string]string `yaml:"env"`
	DependsOn   []string          `yaml:"depends_on"`
	OnFailure   string            `yaml:"on_failure"`
	HealthCheck HealthCheck       `yaml:"health_check"`
}

type HealthCheck struct {
	URL     string  `yaml:"url"`
	Timeout int     `yaml:"timeout"`
	Retries int     `yaml:"retries"`
	Backoff Backoff `yaml:"backoff"`
}

type Backoff struct {
	Initial    float64 `yaml:"initial"`
	Max        float64 `yaml:"max"`
	Multiplier float64 `yaml:"multiplier"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	cfg.fillDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	absDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}
	cfg.resolveWorkingDirs(absDir)

	return &cfg, nil
}

func (c *Config) fillDefaults() {
	for gi := range c.Groups {
		for si := range c.Groups[gi].Services {
			svc := &c.Groups[gi].Services[si]
			if svc.OnFailure == "" {
				svc.OnFailure = "exit"
			}
			if svc.HealthCheck.Timeout == 0 {
				svc.HealthCheck.Timeout = 30
			}
			if svc.HealthCheck.Retries == 0 {
				svc.HealthCheck.Retries = 10
			}
			if svc.HealthCheck.Backoff.Initial == 0 {
				svc.HealthCheck.Backoff.Initial = 1.0
			}
			if svc.HealthCheck.Backoff.Max == 0 {
				svc.HealthCheck.Backoff.Max = 8.0
			}
			if svc.HealthCheck.Backoff.Multiplier == 0 {
				svc.HealthCheck.Backoff.Multiplier = 2.0
			}
		}
	}
}

func (c *Config) validate() error {
	names := map[string]bool{}
	for _, g := range c.Groups {
		for _, svc := range g.Services {
			if svc.Name == "" {
				return fmt.Errorf("service name is required")
			}
			if svc.Command == "" {
				return fmt.Errorf("service %q: command is required", svc.Name)
			}
			if svc.HealthCheck.URL == "" {
				return fmt.Errorf("service %q: health_check.url is required", svc.Name)
			}
			if svc.OnFailure != "exit" && svc.OnFailure != "skip" {
				return fmt.Errorf("service %q: on_failure must be 'exit' or 'skip', got %q", svc.Name, svc.OnFailure)
			}
			if names[svc.Name] {
				return fmt.Errorf("duplicate service name: %q", svc.Name)
			}
			names[svc.Name] = true
		}
	}
	// Validate depends_on references
	for _, g := range c.Groups {
		for _, svc := range g.Services {
			for _, dep := range svc.DependsOn {
				if !names[dep] {
					return fmt.Errorf("service %q: depends_on %q does not exist", svc.Name, dep)
				}
			}
		}
	}
	return nil
}

func (c *Config) resolveWorkingDirs(configDir string) {
	for gi := range c.Groups {
		for si := range c.Groups[gi].Services {
			svc := &c.Groups[gi].Services[si]
			if svc.WorkingDir != "" && !filepath.IsAbs(svc.WorkingDir) {
				svc.WorkingDir = filepath.Join(configDir, svc.WorkingDir)
			}
		}
	}
}

func (c *Config) Flatten() []Service {
	var result []Service
	for _, g := range c.Groups {
		result = append(result, g.Services...)
	}
	return result
}
