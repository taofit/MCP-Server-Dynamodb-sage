package main

import (
	"context"
	"dynamodb-sage/server"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/joho/godotenv"
)

func main() {
	ctx := context.Background()
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("Failed to load .env file: %v", err)
	}
	optFns := []func(*config.LoadOptions) error{}
	if os.Getenv("AWS_REGION") != "" {
		optFns = append(optFns, config.WithRegion(os.Getenv("AWS_REGION")))
	} else {
		log.Fatalf("AWS_REGION is not set")
	}

	// TODO: remove in production - only needed for LocalStack local development
	if os.Getenv("AWS_BASE_ENDPOINT") != "" {
		optFns = append(optFns, config.WithBaseEndpoint(os.Getenv("AWS_BASE_ENDPOINT")))
	} else {
		log.Fatalf("AWS_BASE_ENDPOINT is not set")
	}

	// TODO: remove in production - use IAM roles instead of static credentials
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), "")))
	} else {
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
	}

	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		log.Fatalf("AWS SDK configuration failed: %v", err)
	}
	db := dynamodb.NewFromConfig(cfg)

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/audit_history.db"
	}

	srv := server.New(db, configPath, dbPath)

	port := ":3001"

	http.Handle("/sse", srv.SSEHandler())
	log.Printf("Server started on SSE (port %s)\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
