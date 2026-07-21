package rag

import (
	"dynamodb-sage/internal/engine"
	"strings"
)

type Chunker struct {
	MaxTokens int
	Overlap   int
}

func NewChunker(chunkingconfig engine.ChunkingConfig) *Chunker {
	return &Chunker{
		MaxTokens: chunkingconfig.MaxTokens, 
		Overlap:   chunkingconfig.Overlap,
	}
}

func (c *Chunker) Chunk(text string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	chunkSize := c.MaxTokens
	overlapSize := c.Overlap
	var chunks []string

	for i := 0; i < len(words); i += chunkSize - overlapSize {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
		if chunkSize == overlapSize || i+overlapSize  >= len(words) {
			break
		}
	}
	return chunks
}