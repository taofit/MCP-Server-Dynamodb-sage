package embedding

import (
	"context"
	"dynamodb-sage/internal/awsparam"
	"fmt"
	"os"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"dynamodb-sage/internal/engine"
)

type OpenAIEmbedder struct {
	Client *openai.Client
	Model  string
	Dims   int
}

type Config struct {
	Model   string
	APIKey  string
	BaseURL string
	Dims    int
}

func NewOpenAIEmbedder(ctx context.Context, emBeddConfig engine.EmBeddingConfig) (*OpenAIEmbedder, error) {
	cfg, err := loadConfig(ctx, emBeddConfig)
	if err != nil {
		return nil, err
	}
	return &OpenAIEmbedder{
		Client: openai.Ptr(openai.NewClient(option.WithBaseURL(cfg.BaseURL), option.WithAPIKey(cfg.APIKey))),
		Model:  cfg.Model,
		Dims:   cfg.Dims,
	}, nil
}

func loadConfig(ctx context.Context, emBeddConfig engine.EmBeddingConfig) (*Config, error) {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := emBeddConfig.Model
	dims := emBeddConfig.Dimensions

	if apiKey == "" {
		paramName := os.Getenv("OPENAI_API_KEY_PARAM")
		if paramName == "" {
			paramName = "/dynamodb-sage/openai/api-key"
		}
		var err error
		apiKey, err = awsparam.GetSSMParam(ctx, paramName)
		if err != nil {
			return nil, fmt.Errorf("failed to get API key from SSM (%s): %w", paramName, err)
		}
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &Config{
		Model:   model,
		APIKey:  apiKey,
		BaseURL: baseURL,
		Dims:    dims,
	}, nil
}

func (o *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	resp, err := o.Client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(o.Model),
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(text),
		},
		Dimensions: openai.Int(int64(o.Dims)),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return resp.Data[0].Embedding, nil
}

func (o *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	resp, err := o.Client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(o.Model),
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
		Dimensions: openai.Int(int64(o.Dims)),
	})
	if err != nil {
		return nil, err
	}
	embeddings := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}

func (o *OpenAIEmbedder) Dimensions() int {
	return o.Dims
}