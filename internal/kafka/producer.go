// Package kafka provides a thin wrapper around a Sarama sync producer for enqueuing heavy DynamoDB‑Sage tasks.
package kafka

import "github.com/IBM/sarama"

type Producer interface {
	Send(key string, payload []byte) error
	Close() error
}

type saramaProducer struct {
	producer sarama.SyncProducer
	topic    string
	brokers  []string
}

type SaramaProducerConfig struct {
	Brokers []string
	Topic   string
}

func NewProducer(cfg *SaramaProducerConfig) (Producer, error) {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true
	saramaConfig.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	return &saramaProducer{producer: producer, topic: cfg.Topic, brokers: cfg.Brokers}, nil
}

func (p *saramaProducer) Send(key string, payload []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(payload),
	}

	_, _, err := p.producer.SendMessage(msg)
	return err
}

func (p *saramaProducer) Close() error {
	return p.producer.Close()
}
