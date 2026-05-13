package main

import (
	"context"
	"dynamodb-sage/server"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func main() {
	ctx := context.Background()
	// Configure AWS SDK for LocalStack, for actual AWS, remove WithBaseEndpoint and WithCredentialsProvider, keep ctx only
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("eu-north-1"),
		config.WithBaseEndpoint("http://localhost:4566"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		log.Fatalf("AWS SDK configuration failed: %v", err)
	}
	db := dynamodb.NewFromConfig(cfg)

	srv := server.New(db)

	port := ":3001"
	http.Handle("/sse", srv.SSEHandler())
	log.Printf("Server started on SSE (port %s)\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}