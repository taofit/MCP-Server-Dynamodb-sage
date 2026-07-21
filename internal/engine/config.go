// Package engine provides configuration structures and functions for the DynamoDB Sage engine.
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
	TableSizeMB           float64 `yaml:"table_size_mb"`
	ScanCostUSD           float64 `yaml:"scan_cost_usd"`
	BatchDeleteCount      int32   `yaml:"batch_delete_count"`
	BatchGetCount         int32   `yaml:"batch_get_count"`
	BatchPutCount         int32   `yaml:"batch_put_count"`
	MaxThroughputIncrease int32   `yaml:"max_throughput_increase"`
	UpdateExpressionDepth int32   `yaml:"update_expression_depth"`
}

type AppConfig struct {
	GlobalLimits    GlobalLimits        `yaml:"global_limits"`
	SensitiveFields []string            `yaml:"sensitive_fields"`
	ProtectedTables []string            `yaml:"protected_tables"`
	Tables          []TableConfig       `yaml:"tables"`
	RiskThresholds  RiskThresholds      `yaml:"risk_thresholds"`
	RiskLevel       RiskLevel           `yaml:"risk_level"`
	ProtectedTags   map[string][]string `yaml:"protected_tags"`
	Rag				RagConfig		    `yaml:"rag"`
}

type RagConfig struct {
	Enabled   bool            `yaml:"enabled"`
	Embedding EmBeddingConfig `yaml:"embedding"`
	Chunking  ChunkingConfig  `yaml:"chunking"`
	VectorDB  VectorDBConfig  `yaml:"vector_db"`
	Retrieval RetrievalConfig `yaml:"retrieval"`
}

type ChunkingConfig struct {
	MaxTokens int `yaml:"max_tokens"`
	Overlap int `yaml:"overlap"`
}

type EmBeddingConfig struct {
	Provider string `yaml:"provider"`
	Model string `yaml:"model"`
	Dimensions int `yaml:"dimensions"`
}

type VectorDBConfig struct {
	Provider string `yaml:"provider"`
	Host string `yaml:"host"`
	Port int `yaml:"port"`
}

type RetrievalConfig struct {
	TopK int `yaml:"top_k"`
	ScoreThreshold float64 `yaml:"score_threshold"`
	FinalK int `yaml:"final_k"`
}

type RiskLevel int

const (
	LowRiskLevel      RiskLevel = 0
	MediumRiskLevel   RiskLevel = 1
	HighRiskLevel     RiskLevel = 2
	CriticalRiskLevel RiskLevel = 3
)

const (
	DefaultMaxLimit              int32   = 100
	DefaultLimit                 int32   = 20
	DefaultMaxThroughputIncrease int32   = 2000
	DefaultUpdateExpressionDepth int32   = 5
	DefaultScanCostUSD           float64 = 0.05
	DefaultTableSizeMB           float64 = 10 // 10MB
)

var DefaultProtectedTags = map[string][]string{
	"environment": {"production", "prod"},
}

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

	if config.RiskThresholds.BatchPutCount <= 0 {
		config.RiskThresholds.BatchPutCount = DefaultMaxLimit
	}
	if config.RiskThresholds.BatchDeleteCount <= 0 {
		config.RiskThresholds.BatchDeleteCount = DefaultMaxLimit
	}
	if config.RiskThresholds.BatchGetCount <= 0 {
		config.RiskThresholds.BatchGetCount = DefaultMaxLimit
	}
	if config.RiskThresholds.MaxThroughputIncrease <= 0 {
		config.RiskThresholds.MaxThroughputIncrease = DefaultMaxThroughputIncrease
	}
	if config.RiskThresholds.UpdateExpressionDepth <= 0 {
		config.RiskThresholds.UpdateExpressionDepth = DefaultUpdateExpressionDepth
	}
	if config.RiskThresholds.ScanCostUSD <= 0 {
		config.RiskThresholds.ScanCostUSD = DefaultScanCostUSD
	}
	if config.RiskThresholds.TableSizeMB <= 0 {
		config.RiskThresholds.TableSizeMB = DefaultTableSizeMB
	}
	if len(config.ProtectedTags) == 0 {
		// set default protected tags if not set
		config.ProtectedTags = DefaultProtectedTags
	}
	loadRagConfig(&config)
	return &config, nil
}

func loadRagConfig(appConfig *AppConfig) {
	
	if appConfig.Rag.Embedding.Provider == "" {
		appConfig.Rag.Embedding.Provider = "openai"
	}
	if appConfig.Rag.Embedding.Model == "" {
		appConfig.Rag.Embedding.Model = "text-embedding-3-small"
	}
	if appConfig.Rag.Embedding.Dimensions == 0 {
		appConfig.Rag.Embedding.Dimensions = 1536
	}
	if appConfig.Rag.Chunking.MaxTokens == 0 {
		appConfig.Rag.Chunking.MaxTokens = 500
	}
	if appConfig.Rag.Chunking.Overlap == 0 {
		appConfig.Rag.Chunking.Overlap = 50
	}
	if appConfig.Rag.VectorDB.Provider == "" {
		appConfig.Rag.VectorDB.Provider = "openai"
	}
	if appConfig.Rag.VectorDB.Host == "" {
		appConfig.Rag.VectorDB.Host = "localhost"
	}
	if appConfig.Rag.VectorDB.Port == 0 {
		appConfig.Rag.VectorDB.Port = 6334
	}
	if appConfig.Rag.Retrieval.TopK == 0 {
		appConfig.Rag.Retrieval.TopK = 20
	}
	if appConfig.Rag.Retrieval.ScoreThreshold == 0 {
		appConfig.Rag.Retrieval.ScoreThreshold = 0.75
	}
	if appConfig.Rag.Retrieval.FinalK == 0 {
		appConfig.Rag.Retrieval.FinalK = 4
	}
}
