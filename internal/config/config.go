package config

import (
	"errors"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig   `yaml:"server"`
	Auth      AuthConfig     `yaml:"auth"`
	DB        DBConfig       `yaml:"db"`
	Gateway   GatewayConfig  `yaml:"gateway"`
	Providers []Provider     `yaml:"providers"`
	Keys      []APIKeyConfig `yaml:"keys"`
	Rules     []RoutingRule  `yaml:"rules"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type AuthConfig struct {
	AdminKey string `yaml:"admin_key"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type GatewayConfig struct {
	TimeoutSeconds        int    `yaml:"timeout_seconds"`
	StickySessionHeader   string `yaml:"sticky_session_header"`
	ProviderFailThreshold int    `yaml:"provider_fail_threshold"`
	ProviderCooldownSec   int    `yaml:"provider_cooldown_seconds"`
	Timezone              string `yaml:"timezone"`
	ReloadSeconds         int    `yaml:"reload_seconds"`
}

type Provider struct {
	Name               string            `yaml:"name"`
	BaseURL            string            `yaml:"base_url"`
	APIKey             string            `yaml:"api_key"`
	UpstreamKeys       []string          `yaml:"upstream_keys"`
	Weight             int               `yaml:"weight"`
	Models             []string          `yaml:"models"`
	ModelMap           map[string]string `yaml:"model_map"`
	MaxRPM             int               `yaml:"max_rpm"`
	MaxTPM             int               `yaml:"max_tpm"`
	RequirePathAuth    bool              `yaml:"require_path_auth"`
	Enabled            bool              `yaml:"enabled"`
	GroupName          string            `yaml:"group_name"`
	RecoverIntervalSec int               `yaml:"recover_interval_sec"`
}

type APIKeyConfig struct {
	Key            string   `yaml:"key"`
	Name           string   `yaml:"name"`
	MaxRPM         int      `yaml:"max_rpm"`
	MaxTPM         int      `yaml:"max_tpm"`
	AllowedModels  []string `yaml:"allowed_models"`
	RateMultiplier float64  `yaml:"rate_multiplier"`
}

type RuleMatch struct {
	APIKeys     []string `yaml:"api_keys"`
	ModelPrefix string   `yaml:"model_prefix"`
}

type RuleAction struct {
	ForceProvider  string  `yaml:"force_provider"`
	Deny           bool    `yaml:"deny"`
	RateMultiplier float64 `yaml:"rate_multiplier"`
}

type RoutingRule struct {
	Name   string     `yaml:"name"`
	Match  RuleMatch  `yaml:"match"`
	Action RuleAction `yaml:"action"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	cfg.fillDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) fillDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.DB.Path == "" {
		c.DB.Path = "data/proxy2api.db"
	}
	if c.Gateway.TimeoutSeconds <= 0 {
		c.Gateway.TimeoutSeconds = 60
	}
	if c.Gateway.StickySessionHeader == "" {
		c.Gateway.StickySessionHeader = "session_id"
	}
	if c.Gateway.ProviderFailThreshold <= 0 {
		c.Gateway.ProviderFailThreshold = 3
	}
	if c.Gateway.ProviderCooldownSec <= 0 {
		c.Gateway.ProviderCooldownSec = 30
	}
	if c.Gateway.Timezone == "" {
		c.Gateway.Timezone = "UTC"
	}
	if c.Gateway.ReloadSeconds <= 0 {
		c.Gateway.ReloadSeconds = 10
	}

	for i := range c.Providers {
		if c.Providers[i].Weight <= 0 {
			c.Providers[i].Weight = 1
		}
		if c.Providers[i].GroupName == "" {
			c.Providers[i].GroupName = "default"
		}
		if c.Providers[i].RecoverIntervalSec <= 0 {
			c.Providers[i].RecoverIntervalSec = 60
		}
	}
	for i := range c.Keys {
		if c.Keys[i].RateMultiplier <= 0 {
			c.Keys[i].RateMultiplier = 1
		}
	}
}

func (c *Config) validate() error {
	if len(c.Providers) == 0 {
		return errors.New("at least one provider is required")
	}
	if len(c.Keys) == 0 {
		return errors.New("at least one api key is required")
	}
	for _, p := range c.Providers {
		if p.Name == "" || p.BaseURL == "" {
			return errors.New("provider name and base_url are required")
		}
	}
	for _, k := range c.Keys {
		if k.Key == "" {
			return errors.New("api key value cannot be empty")
		}
	}
	_ = time.Second
	return nil
}
