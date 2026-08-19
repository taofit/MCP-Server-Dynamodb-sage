// Package kafka provides a thin wrapper around a Sarama sync producer for enqueuing heavy DynamoDB‑Sage tasks.
package kafka

import (
	"dynamodb-sage/internal/metrics"
	"encoding/json"
	"strconv"
	"time"

	"github.com/IBM/sarama"
)

const headerSendStart = "x-send-start"
const headerRetryCount = "x-retry-count"
const headerOriginalTopic = "x-original-topic"
const headerErrorMessage = "x-error-message"

type Producer interface {
	Send(topic string, key string, payload []byte) error
	SendToDLQ(key string, payload []byte) error
	Close() error
}

type saramaProducer struct {
	producer sarama.SyncProducer
	brokers  []string
	dlqTopic string
}

type DLQPayload struct {
	OriginalPayload []byte `json:"originalPayload"`
	OriginalTopic   string `json:"originalTopic"`
	RetryCount      int    `json:"retryCount"`
	ErrorMessage    string `json:"errorMessage"`
}

func (p *saramaProducer) SendToDLQ(key string, payload []byte) error {
	var dlqPayload DLQPayload
	if err := json.Unmarshal(payload, &dlqPayload); err != nil {
		return err
	}
	return sendMessage(p.dlqTopic, key, payload, p.producer, []sarama.RecordHeader{
		{Key: []byte(headerOriginalTopic), Value: []byte(dlqPayload.OriginalTopic)},
		{Key: []byte(headerRetryCount), Value: []byte(strconv.Itoa(dlqPayload.RetryCount))},
		{Key: []byte(headerErrorMessage), Value: []byte(dlqPayload.ErrorMessage)},
	})
}

type saramaProducerConfig struct {
	brokers []string
}

func newProducer(cfg *saramaProducerConfig, dlqTopic string) (Producer, error) {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true
	saramaConfig.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(cfg.brokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	return &saramaProducer{producer: producer, brokers: cfg.brokers, dlqTopic: dlqTopic}, nil
}

func (p *saramaProducer) Send(topic string, key string, payload []byte) error {
	return sendMessage(topic, key, payload, p.producer, []sarama.RecordHeader{
		{Key: []byte(headerSendStart), Value: []byte(strconv.FormatInt(time.Now().UnixNano(), 10))},
	})
}

func sendMessage(topic string, key string, payload []byte, producer sarama.SyncProducer, headers []sarama.RecordHeader) error {
	msg := &sarama.ProducerMessage{
		Topic:   topic,
		Key:     sarama.StringEncoder(key),
		Value:   sarama.ByteEncoder(payload),
		Headers: headers,
	}
	_, _, err := producer.SendMessage(msg)
	if err != nil {
		metrics.KafkaSendTotal.WithLabelValues(topic, "error").Inc()
		return err
	}
	metrics.KafkaSendTotal.WithLabelValues(topic, "success").Inc()
	metrics.KafkaSendBytesTotal.WithLabelValues(topic).Add(float64(len(payload)))
	return err
}

func (p *saramaProducer) Close() error {
	return p.producer.Close()
}
