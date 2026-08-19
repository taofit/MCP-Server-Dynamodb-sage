package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"dynamodb-sage/internal/metrics"

	"github.com/IBM/sarama"
)

type Consumer interface {
	Start() error
	GracefulStop() error
	RegisterHandler(topic string, handler Handler)
	SetOnComplete(fn func(key string))
}

type Handler func(key string, payload []byte) error

type saramaConsumer struct {
	producer          Producer
	ready             chan struct{}
	handlersMu        sync.RWMutex
	handlers          map[string]Handler
	onComplete        func(key string)
	brokers           []string
	topics            []string
	consumerGroupName string
	consumerGroup     sarama.ConsumerGroup
	runCancel         context.CancelFunc
	wg                sync.WaitGroup
	dlq               DLQConfig
}

type consumerConfig struct {
	brokers           []string
	topics            []string
	consumerGroupName string
	dlq               DLQConfig
}

func newConsumer(config *consumerConfig, producer Producer) (Consumer, error) {
	handlers := make(map[string]Handler)
	return &saramaConsumer{
		producer:          producer,
		handlers:          handlers,
		brokers:           config.brokers,
		topics:            config.topics,
		consumerGroupName: config.consumerGroupName,
		dlq:               config.dlq,
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

func (c *saramaConsumer) SetOnComplete(fn func(key string)) {
	c.onComplete = fn
}

func (c *saramaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-session.Context().Done():
			return session.Context().Err()
		case message, ok := <-claim.Messages():
			if !ok {
				log.Printf("Message channel was closed")
				return nil
			}
			log.Printf("Message claimed: topic=%s key=%q", message.Topic, string(message.Key))
			instrumentDuration(message, claim)

			// Retrieve handler for the topic
			c.handlersMu.RLock()
			handler, exists := c.handlers[message.Topic]
			c.handlersMu.RUnlock()

			if !exists || handler == nil {
				// No handler registered, just acknowledge the message
				session.MarkMessage(message, "")
				metrics.KafkaProcessedTotal.WithLabelValues(message.Topic, "unhandled").Inc()
				continue
			}

			// Process the message with retry logic
			if err := c.processWithRetry(handler, message); err != nil {
				// Attempt to send to DLQ on processing failure
				dlqPayload := DLQPayload{
					OriginalPayload: message.Value,
					OriginalTopic:   message.Topic,
					RetryCount:      c.dlq.MaxRetries,
					ErrorMessage:    err.Error(),
				}
				dlqBytes, _ := json.Marshal(dlqPayload)
				if dlqErr := c.producer.SendToDLQ(string(message.Key), dlqBytes); dlqErr != nil {
					metrics.KafkaProcessedTotal.WithLabelValues(message.Topic, "dlq_error").Inc()
					session.MarkMessage(message, "")
					return dlqErr
				}
				metrics.KafkaProcessedTotal.WithLabelValues(message.Topic, "dlq").Inc()

			}
			if c.onComplete != nil {
				c.onComplete(string(message.Key))
			}
			session.MarkMessage(message, "")
		}
	}
}

func (c *saramaConsumer) processWithRetry(handler Handler, message *sarama.ConsumerMessage) error {
	retry := 0
	for {
		if err := handler(string(message.Key), message.Value); err != nil {
			if retry >= c.dlq.MaxRetries {
				return fmt.Errorf("max retries reached: %v", err)
			}
			metrics.KafkaProcessedTotal.WithLabelValues(message.Topic, "error").Inc()
			delay := time.Duration(float64(c.dlq.InitialBackoff) * math.Pow(c.dlq.BackoffMultiplier, float64(retry)))
			if delay > c.dlq.MaxBackoff {
				delay = c.dlq.MaxBackoff
			}
			log.Printf("Error processing task key=%s: %v, retry=%d, delay=%s", string(message.Key), err, retry, delay)
			time.Sleep(delay)
			retry++
		} else {
			metrics.KafkaProcessedTotal.WithLabelValues(message.Topic, "success").Inc()
			return nil
		}
	}
}

func instrumentDuration(message *sarama.ConsumerMessage, claim sarama.ConsumerGroupClaim) {
	var sendStart time.Time
	for _, h := range message.Headers {
		if string(h.Key) == headerSendStart {
			if ns, err := strconv.ParseInt(string(h.Value), 10, 64); err == nil {
				sendStart = time.Unix(0, ns)
			}
		}
		if string(h.Key) == headerRetryCount {
			if count, err := strconv.ParseInt(string(h.Value), 10, 64); err == nil {
				metrics.KafkaRetryCount.WithLabelValues(message.Topic, strconv.FormatInt(count, 10)).Inc()
			}
		}
	}
	if !sendStart.IsZero() {
		lag := claim.HighWaterMarkOffset() - message.Offset - 1
		if lag < 0 {
			lag = 0
		}
		partition := claim.Partition()
		dur := time.Since(sendStart).Seconds()
		metrics.KafkaSendDurationSeconds.WithLabelValues(message.Topic).Observe(dur)
		metrics.KafkaConsumerLag.WithLabelValues(message.Topic, strconv.Itoa(int(partition))).Set(float64(lag))
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
