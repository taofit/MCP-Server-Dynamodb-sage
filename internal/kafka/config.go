package kafka

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type KafkaConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Brokers           []string          `yaml:"brokers"`
	Topics            map[string]string `yaml:"topics"`
	ConsumerGroupName string            `yaml:"consumerGroupName"`
	DLQ               DLQConfig         `yaml:"dlq"`
}

type DLQConfig struct {
	Topic             string        `yaml:"topic"`
	MaxRetries        int           `yaml:"maxRetries"`
	InitialBackoff    time.Duration `yaml:"initialBackoff"`
	MaxBackoff        time.Duration `yaml:"maxBackoff"`
	BackoffMultiplier float64       `yaml:"backoffMultiplier"`
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
	if cfg.Topics["notifications"] == "" {
		cfg.Topics["notifications"] = "dynamodb-sage-notifications"
	}
	if cfg.DLQ.Topic == "" {
		cfg.DLQ.Topic = "dynamodb-sage-dlq"
	}
	if cfg.DLQ.MaxRetries == 0 {
		cfg.DLQ.MaxRetries = 3
	}
	if cfg.DLQ.InitialBackoff == 0 {
		cfg.DLQ.InitialBackoff = 1 * time.Second
	}
	if cfg.DLQ.MaxBackoff == 0 {
		cfg.DLQ.MaxBackoff = 30 * time.Second
	}
	if cfg.DLQ.BackoffMultiplier == 0 {
		cfg.DLQ.BackoffMultiplier = 2.0
	}
	return &cfg, nil
}
