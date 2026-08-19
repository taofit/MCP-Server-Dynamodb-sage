package kafka

import (
	"fmt"
)

type Client struct {
	// implements KafkaClient interface in server package
	Producer
	Consumer
	Config *KafkaConfig
}

func NewClient(kafkaConfig *KafkaConfig) (*Client, error) {
	producerConfig := &saramaProducerConfig{
		brokers: kafkaConfig.Brokers,
	}
	p, err := newProducer(producerConfig, kafkaConfig.DLQ.Topic)
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %v", err)
	}
	var topics []string
	for _, topic := range kafkaConfig.Topics {
		if topic != "" {
			topics = append(topics, topic)
		}
	}
	if kafkaConfig.DLQ.Topic != "" {
		topics = append(topics, kafkaConfig.DLQ.Topic)
	}

	c, err := newConsumer(&consumerConfig{
		brokers:           kafkaConfig.Brokers,
		topics:            topics,
		consumerGroupName: kafkaConfig.ConsumerGroupName,
		dlq: DLQConfig{
			Topic:             kafkaConfig.DLQ.Topic,
			MaxRetries:        kafkaConfig.DLQ.MaxRetries,
			InitialBackoff:    kafkaConfig.DLQ.InitialBackoff,
			MaxBackoff:        kafkaConfig.DLQ.MaxBackoff,
			BackoffMultiplier: kafkaConfig.DLQ.BackoffMultiplier,
		},
	}, p)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %v", err)
	}
	return &Client{
		Producer: p,
		Consumer: c,
		Config:   kafkaConfig,
	}, nil
}

func (c *Client) Close() error {
	errs := []error{}
	if err := c.Producer.Close(); err != nil {
		errs = append(errs, fmt.Errorf("error closing producer: %v", err))
	}
	if err := c.Consumer.GracefulStop(); err != nil {
		errs = append(errs, fmt.Errorf("error closing consumer: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("graceful shutdown completed with %d error(s): %v", len(errs), errs)
	}

	return nil
}

func (c *Client) Ping() error {
	if c.Config == nil || len(c.Config.Brokers) == 0 {
		return fmt.Errorf("kafka client not configured")
	}
	producer, err := newProducer(&saramaProducerConfig{
		brokers: c.Config.Brokers,
	}, c.Config.DLQ.Topic)
	if err != nil {
		return fmt.Errorf("failed to create sync producer: %v", err)
	}
	defer producer.Close()
	return nil
}
