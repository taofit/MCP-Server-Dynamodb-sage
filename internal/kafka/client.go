package kafka

import (
	"fmt"
)

type Client struct {
	Producer
	Consumer
}

func NewClient(brokers []string, topic string, consumerGroupName string, processTask func(key string, payload []byte) error) (*Client, error) {
	producerConfig := &SaramaProducerConfig{
		Brokers: brokers,
		Topic:   topic,
	}
	p, err := NewProducer(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %v", err)
	}

	consumerConfig := &ConsumerConfig{
		Brokers:           brokers,
		Topic:             topic,
		ConsumerGroupName: consumerGroupName,
		ProcessTask:       processTask,
	}
	c, err := NewConsumer(consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %v", err)
	}
	return &Client{
		Producer: p,
		Consumer: c,
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
