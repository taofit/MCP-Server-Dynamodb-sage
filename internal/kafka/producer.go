// Package kafka provides a thin wrapper around a Sarama sync producer for enqueuing heavy DynamoDB‑Sage tasks.
package kafka

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"gopkg.in/yaml.v3"
)

type Producer struct {
	producer          sarama.SyncProducer
	topic             string
	brokers           []string
	wg                sync.WaitGroup
	runCancel         context.CancelFunc
	consumerGroup     sarama.ConsumerGroup
	consumerGroupName string
	processTask       func(key string, payload []byte) error
}

type KafkaConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Brokers           []string          `yaml:"brokers"`
	Topics            map[string]string `yaml:"topics"`
	ConsumerGroupName string            `yaml:"consumerGroupName"`
}

type consumer struct {
	ready       chan struct{}
	processTask func(key string, payload []byte) error
}

func (h *consumer) Setup(session sarama.ConsumerGroupSession) error {
	select {
	case h.ready <- struct{}{}:
	default:
	}
	return nil
}

func (h *consumer) Cleanup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message, ok := <-claim.Messages():
			if !ok {
				log.Printf("Message channel was closed")
				return nil
			}
			log.Printf("Message claimed: key = %s, value = %s, topic = %s", string(message.Key), string(message.Value), message.Topic)
			if h.processTask != nil {
				if err := h.processTask(string(message.Key), message.Value); err != nil {
					log.Printf("Error processing task key=%s: %v", string(message.Key), err)
				}
			}
			session.MarkMessage(message, "")
		case <-session.Context().Done():
			return session.Context().Err()
		}
	}
}

func NewProducer(brokers []string, topic string, consumerGroupName string, processTask func(key string, payload []byte) error) (*Producer, error) {
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return nil, err
	}

	return &Producer{producer: producer, topic: topic, brokers: brokers, consumerGroupName: consumerGroupName, processTask: processTask}, nil
}

func (p *Producer) EnqueueTask(key string, payload []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(payload),
	}

	_, _, err := p.producer.SendMessage(msg)
	return err
}

func (p *Producer) Close() error {
	return p.producer.Close()
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
		cfg.Topics["heavy_ops"] = "heavy-operations"
	}
	// if cfg.
	return &cfg, nil
}

func (p *Producer) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	p.runCancel = cancel
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Producer.Return.Successes = true
	config.Producer.Retry.Max = 3
	config.Version = sarama.V2_8_0_0

	// Using broker list from Producer struct (set by NewProducer)
	log.Printf("Kafka consumer initializing with brokers: %v", p.brokers)
	consumerGroup, err := sarama.NewConsumerGroup(p.brokers, p.consumerGroupName, config)
	if err != nil {
		return err
	}
	p.consumerGroup = consumerGroup
	consumer := &consumer{
		ready:       make(chan struct{}, 1),
		processTask: p.processTask,
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			if err := p.consumerGroup.Consume(ctx, []string{p.topic}, consumer); err != nil {
				log.Printf("Error from consumer: %v", err)
				// Add a delay to prevent tight loop on persistent error
				time.Sleep(5 * time.Second)
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	select {
	case <-consumer.ready:
		log.Println("Sarama consumer up and running!...")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for consumer to be ready")
	}
}

func (p *Producer) GracefulStop() error {
	var errs []error
	if p.runCancel != nil {
		p.runCancel()
	}
	p.wg.Wait()
	if err := p.consumerGroup.Close(); err != nil {
		errs = append(errs, fmt.Errorf("error closing consumer group: %v", err))
	}
	if err := p.Close(); err != nil {
		errs = append(errs, fmt.Errorf("error closing producer: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("graceful shutdown completed with %d error(s): %v", len(errs), errs)
	}

	return nil
}
