package llm

import (
	"context"

	"dynamodb-sage/internal/awsparam"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

func envOr(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func LoadConfig(ctx context.Context) (*Config, error) {
	provider := envOr("LLM_PROVIDER", "openai")
	model := envOr("LLM_MODEL", "gpt-4o-mini")
	baseURL := envOr("LLM_BASE_URL", "https://api.openai.com/v1")
	// If the environment variable is set but empty, use the default URL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeoutSecStr := envOr("LLM_TIMEOUT_SEC", "30")
	systemPrompt := envOr("LLM_SYSTEM_PROMPT", DefaultSystemPrompt)

	timeoutSec, err := strconv.Atoi(timeoutSecStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM_TIMEOUT_SEC %s: %w", timeoutSecStr, err)
	}
	
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		paramName := envOr("LLM_API_KEY_PARAM", "/dynamodb-sage/openai/api-key")
		if paramName == "" {
			return nil, errors.New("LLM_API_KEY_PARAM is not set")
		}
		var err error
		apiKey, err = awsparam.GetSSMParam(ctx, paramName)
		if err != nil {
			return nil, fmt.Errorf("failed to get API key from SSM (%s): %w", paramName, err)
		}
	}
	return &Config{
		Provider:     provider,
		APIKey:       apiKey,
		Model:        model,
		BaseURL:      baseURL,
		Timeout:      time.Duration(timeoutSec) * time.Second,
		SystemPrompt: systemPrompt,
	}, nil
}