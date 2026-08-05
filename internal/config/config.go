// Package config loads accounts.yaml + endpoint dictionary (T3).
//
// Schema:
//
//	accounts:
//	  prod:
//	    role_arn: acs:ram::123:role/ai-cloud-ops
//	    regions: [cn-hangzhou, cn-beijing]
//	    endpoint_overrides: {}  # optional per-region overrides
//
// Static endpoint dictionary mirrors design.md § "关键技术决策" — 8 known regions.
package config

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// AccountConfig is per-account configuration.
type AccountConfig struct {
	RoleARN           string            `yaml:"role_arn"`
	Regions           []string          `yaml:"regions"`
	EndpointOverrides map[string]string `yaml:"endpoint_overrides"`
}

// Config is the top-level config.
type Config struct {
	Accounts map[string]AccountConfig `yaml:"accounts"`
}

// Load reads and validates config from YAML.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(c.Accounts) == 0 {
		return nil, fmt.Errorf("config has no accounts")
	}
	for alias, acct := range c.Accounts {
		if acct.RoleARN == "" {
			return nil, fmt.Errorf("account %q: role_arn is empty", alias)
		}
		if len(acct.Regions) == 0 {
			return nil, fmt.Errorf("account %q: regions is empty", alias)
		}
		if slices.Contains(acct.Regions, "") {
			return nil, fmt.Errorf("account %q: empty region ID", alias)
		}
	}
	return &c, nil
}

// Account returns the account by alias; ok=false if not present.
func (c *Config) Account(alias string) (AccountConfig, bool) {
	acct, ok := c.Accounts[alias]
	return acct, ok
}

// DefaultEndpoints is the static endpoint dictionary for known regions.
// Mirrors ai_cloud_ops.config.DEFAULT_ENDPOINTS — keep in sync.
var DefaultEndpoints = map[string]string{
	// CloudMonitor
	"cn-hangzhou":    "cms.cn-hangzhou.aliyuncs.com",
	"cn-beijing":     "cms.cn-beijing.aliyuncs.com",
	"cn-shanghai":    "cms.cn-shanghai.aliyuncs.com",
	"cn-shenzhen":    "cms.cn-shenzhen.aliyuncs.com",
	"cn-qingdao":     "cms.cn-qingdao.aliyuncs.com",
	"cn-hongkong":    "cms.cn-hongkong.aliyuncs.com",
	"ap-southeast-1": "cms.ap-southeast-1.aliyuncs.com",
	"ap-southeast-5": "cms.ap-southeast-5.aliyuncs.com",
	"us-west-1":      "cms.us-west-1.aliyuncs.com",
	// STS endpoint — region-agnostic
	"_sts_default": "sts.cn-hangzhou.aliyuncs.com",
}

// EndpointFor returns the endpoint for (service, region) for an account.
// Per-account override takes precedence over the static dictionary.
func EndpointFor(service, region string, account AccountConfig) (string, error) {
	if service == "sts" {
		return DefaultEndpoints["_sts_default"], nil
	}
	overrideKey := service + "." + region
	if ep, ok := account.EndpointOverrides[overrideKey]; ok {
		return ep, nil
	}
	if ep, ok := DefaultEndpoints[region]; ok {
		return ep, nil
	}
	return "", fmt.Errorf("no endpoint known for %s in %s; add to account.endpoint_overrides or DefaultEndpoints", service, region)
}
