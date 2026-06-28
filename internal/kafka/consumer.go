package kafka

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

type Consumer interface {
	Start() error
	GracefulStop() error
	RegisterHandler(topic string, handler Handler)
}

type Handler func(key string, payload []byte) error

type saramaConsumer struct {
	ready             chan struct{}
	handlersMu        sync.RWMutex
	handlers          map[string]Handler
	brokers           []string
	topics            []string
	consumerGroupName string
	consumerGroup     sarama.ConsumerGroup
	runCancel         context.CancelFunc
	wg                sync.WaitGroup
}

type consumerConfig struct {
	brokers           []string
	topics            []string
	consumerGroupName string
}

func newConsumer(config *consumerConfig) (Consumer, error) {
	handlers := make(map[string]Handler)
	return &saramaConsumer{
		handlers:          handlers,
		brokers:           config.brokers,
		topics:            config.topics,
		consumerGroupName: config.consumerGroupName,
		ready:             make(chan struct{}, 1),
	}, nil
}

func (c *saramaConsumer) Setup(session sarama.ConsumerGroupSession) error {
	select {
	case c.ready <- struct{}{}:
	default:
	}
	return nil
}

func (c *saramaConsumer) Cleanup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (c *saramaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message, ok := <-claim.Messages():
			if !ok {
				log.Printf("Message channel was closed")
				return nil
			}
			log.Printf("Message claimed: topic=%s key=%q", message.Topic, string(message.Key))
			c.handlersMu.RLock()
			handler, ok := c.handlers[message.Topic]
			c.handlersMu.RUnlock()
			if ok && handler != nil {
				if err := handler(string(message.Key), message.Value); err != nil {
					log.Printf("Error processing task key=%s: %v", string(message.Key), err)
				}
			}
			session.MarkMessage(message, "")
		case <-session.Context().Done():
			return session.Context().Err()
		}
	}
}

func (c *saramaConsumer) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	c.runCancel = cancel
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Producer.Return.Successes = true
	config.Producer.Retry.Max = 3
	config.Version = sarama.V2_8_0_0
	config.Consumer.Group.Session.Timeout = 30 * time.Second // default is 10 s
	config.Consumer.Group.Heartbeat.Interval = 3 * time.Second

	log.Printf("Kafka consumer initializing with brokers: %v", c.brokers)
	consumerGroup, err := sarama.NewConsumerGroup(c.brokers, c.consumerGroupName, config)
	if err != nil {
		return err
	}
	c.consumerGroup = consumerGroup
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			if err := c.consumerGroup.Consume(ctx, c.topics, c); err != nil {
				log.Printf("Error from consumer: %v", err)
				time.Sleep(5 * time.Second)
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	select {
	case <-c.ready:
		log.Println("Sarama consumer up and running!...")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for consumer to be ready")
	}
}

func (c *saramaConsumer) RegisterHandler(topic string, handler Handler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[topic] = handler
}

func (c *saramaConsumer) GracefulStop() error {
	var errs []error
	if c.runCancel != nil {
		c.runCancel()
	}
	if err := c.consumerGroup.Close(); err != nil {
		errs = append(errs, fmt.Errorf("error closing consumer group: %v", err))
	}
	c.wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("graceful shutdown completed with %d error(s): %v", len(errs), errs)
	}

	return nil
}
