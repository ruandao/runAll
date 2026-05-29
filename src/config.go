package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"runAll/src/domain"
)

type Config struct {
	Version       string        `yaml:"version"`
	Logging       Logging       `yaml:"logging"`
	Observability Observability `yaml:"observability"`
	Groups        []Group       `yaml:"groups"`
}

type Logging struct {
	FileRoot string `yaml:"file_root"`
}

type Observability struct {
	GrafanaURL        string `yaml:"grafana_url"`
	LokiURL           string `yaml:"loki_url"`
	TraceDashboardUID string `yaml:"trace_dashboard_uid"`
}

type Group struct {
	Name     string    `yaml:"name"`
	Services []Service `yaml:"services"`
}

type Service struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	BuildCommand string            `yaml:"build_command"` // optional, runs before restart
	Language     string            `yaml:"language"`      // optional, shown in UI; auto-detected when empty
	WorkingDir   string            `yaml:"working_dir"`
	Env          map[string]string `yaml:"env"`
	DependsOn    []string          `yaml:"depends_on"`
	OnFailure    string            `yaml:"on_failure"`
	HealthCheck  HealthCheck       `yaml:"health_check"`
}

type HealthCheck struct {
	URL                string  `yaml:"url"`
	LivenessURL        string  `yaml:"liveness_url"` // optional; startup probe only (readiness uses url)
	Timeout            int     `yaml:"timeout"`
	Retries            int     `yaml:"retries"`
	CheckInterval      int     `yaml:"check_interval"`      // seconds between continuous health pings
	UnhealthyThreshold int     `yaml:"unhealthy_threshold"` // consecutive failures before marking unhealthy
	Backoff            Backoff `yaml:"backoff"`
}

// StartupProbeURL returns the HTTP URL used while waiting for a service to become ready.
// When liveness_url is set, startup waits only for the process to serve HTTP (fast path).
// Continuous monitoring and dependency semantics still use url (readiness).
func (hc HealthCheck) StartupProbeURL() string {
	if u := strings.TrimSpace(hc.LivenessURL); u != "" {
		return u
	}
	return hc.URL
}

// ReadinessProbeURL returns the URL for ongoing readiness monitoring.
func (hc HealthCheck) ReadinessProbeURL() string {
	return hc.URL
}

// HasSplitProbe reports whether startup (liveness) and monitoring (readiness) use different URLs.
func (hc HealthCheck) HasSplitProbe() bool {
	live := strings.TrimSpace(hc.LivenessURL)
	ready := strings.TrimSpace(hc.ReadinessProbeURL())
	return live != "" && ready != "" && live != ready
}

// StartupProbeConfig returns a HealthCheck copy that probes liveness during startup.
func (hc HealthCheck) StartupProbeConfig() HealthCheck {
	probe := hc
	probe.URL = hc.StartupProbeURL()
	return probe
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

func LoadConfigWithSourceGuard(primaryPath, secondaryPath string) (*Config, domain.ConfigFingerprint, error) {
	emptyFingerprint := domain.ConfigFingerprint{}

	primaryHash, err := fileSHA256(primaryPath)
	if err != nil {
		return nil, emptyFingerprint, fmt.Errorf("hash primary config: %w", err)
	}

	if strings.TrimSpace(secondaryPath) != "" {
		secondaryHash, err := fileSHA256(secondaryPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, emptyFingerprint, fmt.Errorf("hash secondary config: %w", err)
		}
		if err == nil && secondaryHash != primaryHash {
			return nil, emptyFingerprint, fmt.Errorf("config source mismatch: primary=%s secondary=%s", primaryPath, secondaryPath)
		}
	}

	fingerprint, err := domain.NewConfigFingerprint(primaryPath, primaryHash)
	if err != nil {
		return nil, emptyFingerprint, fmt.Errorf("create config fingerprint: %w", err)
	}

	cfg, err := LoadConfig(primaryPath)
	if err != nil {
		return nil, emptyFingerprint, err
	}

	return cfg, fingerprint, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (c *Config) fillDefaults() {
	c.Logging.FileRoot = resolveLogFileRoot(c.Logging.FileRoot)
	c.Observability = resolveObservability(c.Observability)
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
			if svc.HealthCheck.CheckInterval == 0 {
				svc.HealthCheck.CheckInterval = 10
			}
			if svc.HealthCheck.UnhealthyThreshold == 0 {
				svc.HealthCheck.UnhealthyThreshold = 2
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

func resolveLogFileRoot(configured string) string {
	if env := strings.TrimSpace(os.Getenv("RUNALL_LOG_ROOT")); env != "" {
		return env
	}
	return strings.TrimSpace(configured)
}

func resolveObservability(o Observability) Observability {
	if env := strings.TrimSpace(os.Getenv("GRAFANA_URL")); env != "" {
		o.GrafanaURL = env
	}
	if env := strings.TrimSpace(os.Getenv("LOKI_URL")); env != "" {
		o.LokiURL = env
	}
	if strings.TrimSpace(o.GrafanaURL) == "" {
		o.GrafanaURL = "http://127.0.0.1:3000"
	}
	if strings.TrimSpace(o.LokiURL) == "" {
		o.LokiURL = "http://127.0.0.1:3100"
	}
	if strings.TrimSpace(o.TraceDashboardUID) == "" {
		o.TraceDashboardUID = "distributed-trace-view"
	}
	return o
}
