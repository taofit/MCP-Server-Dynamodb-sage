package llm

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

type Message struct {
	Role        string
	Content     string
	ToolCalls   []ToolCall   // assistant messages with tool_use blocks
	ToolResults []ToolResult // user messages carrying tool_results
}

type Client struct {
	claudeClient anthropic.Client
	model        string
	timeout      time.Duration
	systemPrompt string
	maxTokens    int64
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage // Can be parsed to JSON
}

type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON schema: {"type":"object","properties":{...},"required":[...]}
}

type ToolResult struct {
	ToolCallID  string
	DisplayName string
	Result      string
	IsError     bool
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
		option.WithMaxRetries(0),
	}

	maxTokens := int64(config.MaxTokens)
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}

	return &Client{
		claudeClient: anthropic.NewClient(opts...),
		model:        config.Model,
		timeout:      config.Timeout,
		systemPrompt: config.SystemPrompt,
		maxTokens:    maxTokens,
	}, nil
}

func (c *Client) Close() {}

func (c *Client) Model() string {
	return c.model
}

func (c *Client) Generate(ctx context.Context, systemPrompt string, messages []Message, tools []ToolDef, tokenChan chan string) ([]ToolCall, error) {
	msgs := constructMessages(messages)
	return c.generateStream(ctx, systemPrompt, msgs, tools, tokenChan)
}

func constructMessages(messages []Message) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(messages))
	for _, message := range messages {
		switch {
		case message.Role == "assistant" && len(message.ToolCalls) > 0:
			capacity := len(message.ToolCalls)
			if strings.TrimSpace(message.Content) != "" {
				capacity++
			}
			content := make([]anthropic.ContentBlockParamUnion, 0, capacity)
			for _, tc := range message.ToolCalls {
				b := anthropic.NewToolUseBlock(tc.ID, tc.Arguments, tc.Name)
				content = append(content, b)
			}
			if strings.TrimSpace(message.Content) != "" {
				content = append(content, anthropic.NewTextBlock(message.Content))
			}
			result = append(result, anthropic.NewAssistantMessage(content...))
		case message.Role == "user" && len(message.ToolResults) > 0:
			capacity := len(message.ToolResults)
			if strings.TrimSpace(message.Content) != "" {
				capacity++
			}
			content := make([]anthropic.ContentBlockParamUnion, 0, capacity)
			for _, tr := range message.ToolResults {
				b := anthropic.NewToolResultBlock(tr.ToolCallID, tr.Result, tr.IsError)
				content = append(content, b)
			}
			result = append(result, anthropic.NewUserMessage(content...))
		case strings.TrimSpace(message.Content) != "":
			content := anthropic.NewTextBlock(message.Content)
			switch message.Role {
			case "assistant":
				result = append(result, anthropic.NewAssistantMessage(content))
			case "user":
				result = append(result, anthropic.NewUserMessage(content))
			default:
				result = append(result, anthropic.NewUserMessage(content))
			}
		}
	}
	return result
}

// GenerateStream streams text tokens to tokenChan.
// The caller is responsible for closing tokenChan when done.
func (c *Client) generateStream(ctx context.Context, systemPrompt string, messages []anthropic.MessageParam, tools []ToolDef, tokenChan chan string) ([]ToolCall, error) {
	params := c.generateParams(messages, systemPrompt, tools)
	stream := c.claudeClient.Messages.NewStreaming(ctx, params)
	defer stream.Close() //nolint:errcheck

	var msg anthropic.Message
	var streamErr error
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return nil, err
		}
		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch delta := eventVariant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if delta.Text != "" {
					tokenChan <- delta.Text
				}
			}
		}
	}
	if err := stream.Err(); err != nil && err != io.EOF {
		streamErr = err
	}
	var toolCalls []ToolCall
	for _, content := range msg.Content {
		if toolUse, ok := content.AsAny().(anthropic.ToolUseBlock); ok {
			toolCalls = append(toolCalls, ToolCall{
				ID:        toolUse.ID,
				Name:      toolUse.Name,
				Arguments: toolUse.Input,
			})
		}
	}
	// NOTE: caller is responsible for closing tokenChan.
	return toolCalls, streamErr
}

func (c *Client) generateParams(messages []anthropic.MessageParam, systemPrompt string, tools []ToolDef) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     c.model,
		Messages:  messages,
		MaxTokens: c.maxTokens,
	}
	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemPrompt},
		}
	}
	params.Tools = convertToolDefs(tools)
	return params
}

func convertToolDefs(toolDefs []ToolDef) []anthropic.ToolUnionParam {
	tools := make([]anthropic.ToolUnionParam, len(toolDefs))
	for i, tool := range toolDefs {
		properties := map[string]any{}
		if p, ok := tool.InputSchema["properties"]; ok {
			if m, ok := p.(map[string]any); ok {
				properties = m
			}
		}
		var required []string
		if r, ok := tool.InputSchema["required"]; ok {
			required, _ = r.([]string)
		}
		tools[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        tool.Name,
				Type:        anthropic.ToolTypeCustom,
				Description: param.NewOpt(tool.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: properties,
					Required:   required,
				},
			},
		}
	}
	return tools
}

func (c *Client) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream := c.claudeClient.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model: c.model,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock("Hello"),
			),
		},
		MaxTokens: 10,
	})
	defer stream.Close()
	for stream.Next() {
	}
	if err := stream.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}
