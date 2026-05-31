package engine

import (
	"os"

	"gopkg.in/yaml.v3"
)

type TableConfig struct {
	Name          string            `yaml:"name"`
	PIIFields     []string          `yaml:"pii_fields"`
	ReadOnly      bool              `yaml:"read_only"`
	EnforceSchema bool              `yaml:"enforce_schema"`
	Columns       map[string]string `yaml:"columns"`
}

type GlobalLimits struct {
	MaxLimit     int32 `yaml:"max_limit"`
	DefaultLimit int32 `yaml:"default_limit"`
}

type RiskThresholds struct {
	TableSizeMB                float64 `yaml:"table_size_mb"`
	ScanCostUSD                float64 `yaml:"scan_cost_usd"`
	BulkDeleteCount            int     `yaml:"bulk_delete_count"`
	BatchGetItemCountThreshold int     `yaml:"batch_get_item_count_threshold"`
}

type AppConfig struct {
	GlobalLimits    GlobalLimits   `yaml:"global_limits"`
	SensitiveFields []string       `yaml:"sensitive_fields"`
	ProtectedTables []string       `yaml:"protected_tables"`
	Tables          []TableConfig  `yaml:"tables"`
	RiskThresholds  RiskThresholds `yaml:"risk_thresholds"`
	RiskLevel       RiskLevel      `yaml:"risk_level"`
}

type RiskLevel int

const (
	LowRiskLevel      RiskLevel = 0
	MediumRiskLevel   RiskLevel = 1
	HighRiskLevel     RiskLevel = 2
	CriticalRiskLevel RiskLevel = 3
)

const (
	DefaultMaxLimit int32 = 100
	DefaultLimit    int32 = 20
)

func LoadConfig(filename string) (*AppConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.GlobalLimits.MaxLimit <= 0 || config.GlobalLimits.MaxLimit > DefaultMaxLimit {
		config.GlobalLimits.MaxLimit = DefaultMaxLimit
	}

	if config.GlobalLimits.DefaultLimit <= 0 || config.GlobalLimits.DefaultLimit > DefaultMaxLimit {
		config.GlobalLimits.DefaultLimit = DefaultLimit
	}
	// Ensure DefaultLimit does not exceed MaxLimit
	if config.GlobalLimits.DefaultLimit > config.GlobalLimits.MaxLimit {
		config.GlobalLimits.DefaultLimit = config.GlobalLimits.MaxLimit
	}

	return &config, nil
}
