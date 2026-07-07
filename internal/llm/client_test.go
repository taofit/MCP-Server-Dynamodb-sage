package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func TestClient_Generate(t *testing.T) {
	// 1. Setup mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("expected Authorization header to start with Bearer")
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		// Verify model
		if reqBody["model"] != "mock-model" {
			t.Errorf("expected model 'mock-model', got %v", reqBody["model"])
		}

		// Respond with mock completions
		w.Header().Set("Content-Type", "application/json")
		resp := mockChatCompletionResponse{
			ID:      "chatcmpl-mock",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "mock-model",
		}
		resp.Choices = append(resp.Choices, struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			Index: 0,
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: "Hello from mock LLM!",
			},
			FinishReason: "stop",
		})

		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	// 2. Initialize LLM Client
	config := Config{
		Provider:     "openai",
		APIKey:       "mock-key",
		Model:        "mock-model",
		BaseURL:      mockServer.URL,
		Timeout:      2 * time.Second,
		SystemPrompt: "Mock Prompt",
	}

	client, err := NewClientFromConfig(&config)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 3. Call Generate
	ctx := context.Background()
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	resp, err := client.Generate(ctx, "Mock Prompt", messages)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp != "Hello from mock LLM!" {
		t.Errorf("expected 'Hello from mock LLM!', got '%s'", resp)
	}
}

func TestClient_Generate_Timeout(t *testing.T) {
	// 1. Setup mock HTTP server that hangs
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // sleep longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	// 2. Initialize LLM Client with a short timeout
	config := Config{
		Provider:     "openai",
		APIKey:       "mock-key",
		Model:        "mock-model",
		BaseURL:      mockServer.URL,
		Timeout:      100 * time.Millisecond,
		SystemPrompt: "Mock Prompt",
	}

	client, err := NewClientFromConfig(&config)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 3. Call Generate and expect context deadline exceeded error
	ctx := context.Background()
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	_, err = client.Generate(ctx, "Mock Prompt", messages)
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected timeout or canceled error, got: %v", err)
	}
}
