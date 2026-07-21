package server

import (
	"dynamodb-sage/internal/kafka"
	"fmt"
	"log"
	"os"
)

type KafkaClient interface {
	Send(topic string, key string, value []byte) error
	Start() error
	Ping() error
	RegisterHandler(topic string, handler kafka.Handler)
	Close() error
}

func (srv *Server) initKafkaClient(kafkaConfigPath string) error {
	kafkaConfig, err := kafka.LoadConfig(kafkaConfigPath)
	if err != nil {
		return err
	}
	if !kafkaConfig.Enabled {
		return fmt.Errorf("kafka client disabled")
	}

	if os.Getenv("AWS_BASE_ENDPOINT") != "" {
		kafkaConfig.ConsumerGroupName = fmt.Sprintf("%s-%d", kafkaConfig.ConsumerGroupName, os.Getpid())
	}

	kafkaClient, err := kafka.NewClient(kafkaConfig)
	if err != nil {
		return err
	}

	srv.kafkaClient = kafkaClient
	srv.heavyOpsTopic = kafkaConfig.Topics["heavy_ops"]
	srv.notificationsTopic = kafkaConfig.Topics["notifications"]
	srv.kafkaClient.RegisterHandler(srv.heavyOpsTopic, srv.processHeavyOp)
	srv.kafkaClient.RegisterHandler(srv.notificationsTopic, srv.processNotification)
	go func() {
		if err := srv.kafkaClient.Start(); err != nil {
			log.Printf("Failed to start kafka client: %v", err)
		}
	}()
	return nil
}
