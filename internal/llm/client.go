package llm

import (
	"context"
	"time"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

type Message struct {
	Role    string
	Content string
}

type Client struct {
	sdk          openai.Client
	model        string
	timeout      time.Duration
	systemPrompt string
}

func (c *Client) LoadSystemPrompt() string {
	return c.systemPrompt
}

func NewClient(ctx context.Context) (*Client, error) {
	config, err := LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return NewClientFromConfig(config)
}

func NewClientFromConfig(config *Config) (*Client, error) {
	opts := []option.RequestOption{
		option.WithBaseURL(config.BaseURL),
		option.WithAPIKey(config.APIKey),
		option.WithMaxRetries(3),
	}

	return &Client{
		sdk:          openai.NewClient(opts...),
		model:        config.Model,
		timeout:      config.Timeout,
		systemPrompt: config.SystemPrompt,
	}, nil
}

func (c *Client) Close() {
	// todo: close the client
}

func (c *Client) Model() string {
	return c.model
}

func (c *Client) Generate(ctx context.Context, systemPrompt string, messages []Message) (string, error) {
	chatMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if systemPrompt != "" {
		chatMessages = append(chatMessages, openai.DeveloperMessage(systemPrompt))
	}
	for _, message := range messages {
		if message.Role == "assistant" {
			chatMessages = append(chatMessages, openai.AssistantMessage(message.Content))
		} else {
			chatMessages = append(chatMessages, openai.UserMessage(message.Content))
		}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	r, err := c.sdk.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: chatMessages,
		Model:    shared.ChatModel(c.model),
	})

	if err != nil {
		return "", err
	}

	return r.Choices[0].Message.Content, nil
}
