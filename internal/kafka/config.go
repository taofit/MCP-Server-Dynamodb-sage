package kafka

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type KafkaConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Brokers           []string          `yaml:"brokers"`
	Topics            map[string]string `yaml:"topics"`
	ConsumerGroupName string            `yaml:"consumerGroupName"`
}

func LoadConfig(configPath string) (*KafkaConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg KafkaConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if brokerEn := os.Getenv("KAFKA_BROKERS"); brokerEn != "" {
		cfg.Brokers = strings.Split(brokerEn, ",")
	} else if len(cfg.Brokers) == 0 {
		cfg.Brokers = []string{"kafka:9092"}
	}
	if cfg.ConsumerGroupName == "" {
		cfg.ConsumerGroupName = "dynamodb-sage"
	}
	if cfg.Topics == nil {
		cfg.Topics = make(map[string]string)
	}
	if cfg.Topics["heavy_ops"] == "" {
		cfg.Topics["heavy_ops"] = "dynamodb-sage-heavy-ops"
	}
	if cfg.Topics["audit_log"] == "" {
		cfg.Topics["audit_log"] = "dynamodb-sage-audit-log"
	}
	if cfg.Topics["notifications"] == "" {
		cfg.Topics["notifications"] = "dynamodb-sage-notifications"
	}
	return &cfg, nil
}
