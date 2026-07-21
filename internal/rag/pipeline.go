// Package rag provides RAG pipeline implementation
package rag

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"dynamodb-sage/internal/embedding"
	"dynamodb-sage/internal/engine"
	"dynamodb-sage/internal/vector"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	Dimensions() int
}

type VectorDB interface {
	UpsertPoints(ctx context.Context, collection string, points []vector.Point) error
	Search(ctx context.Context, collection string, vector []float32, limit int) ([]vector.ScoredPoint, error)
	DeletePoints(ctx context.Context, collection string, ids []uint64) error
	CreateCollection(ctx context.Context, collection string, dims int) error
}

type RagPipeline struct {
	embedder  Embedder
	chunker   *Chunker
	vectorDB  VectorDB
	retrieval engine.RetrievalConfig
}

func NewRagPipeline(ragConfig engine.RagConfig) (*RagPipeline, error) {
	if !ragConfig.Enabled {
		return nil, fmt.Errorf("rag is not configured")
	}
	ctx := context.Background()
	openaiEmbedder, err := embedding.NewOpenAIEmbedder(ctx, ragConfig.Embedding)
	if err != nil {
		return nil, err
	}

	chunker := NewChunker(ragConfig.Chunking)
	qdrant, err := vector.NewQdrantClient(ragConfig.VectorDB)
	if err != nil {
		return nil, err
	}

	return &RagPipeline{
		embedder:  openaiEmbedder,
		chunker:   chunker,
		vectorDB:  qdrant,
		retrieval: ragConfig.Retrieval,
	}, nil
}

func (r *RagPipeline) ProcessDocument(ctx context.Context, collection string, documentID string, textField string, document string) error {
	// Chunk the document
	chunks := r.chunker.Chunk(document)

	// Embed the chunks
	embeddings, err := r.embedder.EmbedBatch(ctx, chunks)
	if err != nil {
		return err
	}
	float32Embeddings := make([][]float32, len(embeddings))
	for i, emb := range embeddings {
		float32Embeddings[i] = make([]float32, len(emb))
		for j, v := range emb {
			float32Embeddings[i][j] = float32(v)
		}
	}

	// Upsert into vector database
	points := make([]vector.Point, len(chunks))
	for i, chunk := range chunks {
		points[i] = vector.Point{
			ID:       hashKey(documentID + textField + strconv.Itoa(i)),
			Vector:   float32Embeddings[i],
			Payload:  map[string]string{
				"chunk": chunk,
				"source": collection,
				"document": documentID,
			},
		}
	}

	return r.vectorDB.UpsertPoints(ctx, collection, points)
}

func hashKey(key string) uint64 {
	h := sha256.Sum256([]byte(key))
	return binary.LittleEndian.Uint64(h[:8])
}

type SearchResult struct {
	Chunk    string
	Source   string
	Document string
	Score    float64
}

func (r *RagPipeline) Search(ctx context.Context, collection string, query string, filter string, topK int, scoreThreshold float64, finalK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = r.retrieval.TopK
	}
	if scoreThreshold <= 0 {
		scoreThreshold = r.retrieval.ScoreThreshold
	}
	if finalK <= 0 {
		finalK = r.retrieval.FinalK
	}
	embedding, err := r.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	float32Embed := make([]float32, len(embedding))
	for i, v := range embedding {
		float32Embed[i] = float32(v)
	}

	results, err := r.vectorDB.Search(ctx, collection, float32Embed, topK)
	if err != nil {
		return nil, err
	}

	var filtered []SearchResult
	for _, r := range results {
		if r.Score < scoreThreshold {
			continue
		}
		filtered = append(filtered, SearchResult{
			Chunk:    r.Payload["chunk"],
			Source:   r.Payload["source"],
			Document: r.Payload["document"],
			Score:    r.Score,
		})
	}

	if len(filtered) > finalK {
		filtered = filtered[:finalK]
	}
	return filtered, nil
}

func (r *RagPipeline) EnsureCollection(ctx context.Context, collection string) error {
	dims := r.embedder.Dimensions()
	err := r.vectorDB.CreateCollection(ctx, collection, dims)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}