package llm

import (
	"context"

	"dynamodb-sage/internal/awsparam"
	"fmt"
	"os"
	"strconv"
	"time"
)

const DefaultMaxTokens = 1024 * 4

func LoadConfig(ctx context.Context) (*Config, error) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeoutSecStr := os.Getenv("LLM_TIMEOUT_SEC")
	if timeoutSecStr == "" {
		timeoutSecStr = "30"
	}

	timeoutSec, err := strconv.Atoi(timeoutSecStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM_TIMEOUT_SEC %s: %w", timeoutSecStr, err)
	}
	
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		paramName := os.Getenv("LLM_API_KEY_PARAM")
		if paramName == "" {
			paramName = "/dynamodb-sage/claude/api-key"
		}
		var err error
		apiKey, err = awsparam.GetSSMParam(ctx, paramName)
		if err != nil {
			return nil, fmt.Errorf("failed to get API key from SSM (%s): %w", paramName, err)
		}
	}
	return &Config{
		APIKey:       apiKey,
		Model:        model,
		BaseURL:      baseURL,
		Timeout:      time.Duration(timeoutSec) * time.Second,
		SystemPrompt: DefaultSystemPrompt,
		MaxTokens:    DefaultMaxTokens,
	}, nil
}