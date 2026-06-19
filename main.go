package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"dynamodb-sage/server"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("warning: failed to load .env file: %v", err)
	}

	endpoint := os.Getenv("AWS_BASE_ENDPOINT")
	akid := os.Getenv("AWS_ACCESS_KEY_ID")
	sak := os.Getenv("AWS_SECRET_ACCESS_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	optFns := []func(*config.LoadOptions) error{}
	if endpoint != "" {
		log.Printf("Development mode: routing AWS traffic to Localstack endpoint: %s", endpoint)
		if akid == "" || sak == "" {
			akid, sak = "test", "test"
		}
		optFns = append(optFns, config.WithBaseEndpoint(endpoint))
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(akid, sak, "")))
	} else {
		log.Println("Production mode: using default credential chain (IAM roles, env vars, ~/.aws/credentials)")
	}

	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	// Resolve caller identity for audit logging via AWS STS.
	userID := "unknown"
	userARN := "unknown"
	stsClient := sts.NewFromConfig(cfg)
	caller, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err == nil {
		userID = *caller.UserId
		userARN = *caller.Arn
		// Extract friendly name from ARN
		//   arn:aws:iam::account:user/name      → name
		//   arn:aws:sts::account:assumed-role/n  → n
		//   arn:aws:iam::account:root            → root
		if s := strings.SplitN(userARN, "/", 2); len(s) > 1 {
			userID = s[1]
		} else if s := strings.Split(userARN, ":"); len(s) > 0 {
			userID = s[len(s)-1]
		}
	} else {
		log.Printf("warning: failed to get caller identity: %v", err)
		if endpoint == "" {
			userID = akid
		}
	}

	log.Printf("Authenticated Principal: %s (Context Identifier: %s)", userID, userARN)

	dbPath := "data/audit.db"
	if v := os.Getenv("DYNAMO_SAGE_DB"); v != "" {
		dbPath = v
	}
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("failed to create data directory for %s: %v", dbDir, err)
	}

	configPath := "config.yaml"
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		configPath = v
	}

	kafkaConfigPath := "config/kafka.yaml"
	if v := os.Getenv("KAFKA_CONFIG_PATH"); v != "" {
		kafkaConfigPath = v
	}
	db := dynamodb.NewFromConfig(cfg)
	srv := server.New(db, userID, userARN, configPath, kafkaConfigPath, dbPath)

	transportMode := os.Getenv("MCP_TRANSPORT_MODE")
	if transportMode == "" {
		transportMode = "stdio"
	}

	switch transportMode {
	case "stdio":
		if err := srv.ServeStdio(); err != nil {
			log.Fatalf("failed to serve dynamo-sage MCP server on standard IO: %v", err)
		}
	case "http":
		go func() {
			if err := srv.ServeHTTP(":8080"); err != nil {
				log.Fatalf("failed to serve dynamo-sage MCP server on HTTP: %v", err)
			}
		}()
	default:
		log.Fatalf("invalid MCP transport mode: %s", transportMode)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down gracefully...")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}
