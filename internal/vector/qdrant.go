package vector

import (
	"context"
	"dynamodb-sage/internal/engine"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/qdrant/go-client/qdrant"
)

type QdrantClient struct {
	client *qdrant.Client
}

type Point struct {
	ID      uint64
	Vector  []float32
	Payload map[string]string
}

type ScoredPoint struct {
	ID      uint64
	Score   float64
	Payload map[string]string
}

func NewQdrantClient(cfg engine.VectorDBConfig) (*QdrantClient, error) {
	if os.Getenv("QDRANT_HOST") != "" {
		cfg.Host = os.Getenv("QDRANT_HOST")
	}
	if cfg.Port == 0 {
		cfg.Port = 6334
	}
	client, err := qdrant.NewClient(
		&qdrant.Config{
			Host: cfg.Host,
			Port: cfg.Port,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	return &QdrantClient{client: client}, nil
}

func (c *QdrantClient) CreateCollection(ctx context.Context, collectionName string, dims int) error {
	vectorsParams := qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     uint64(dims),
		Distance: qdrant.Distance_Cosine,
	})
	err := c.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig:  vectorsParams,
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}
	return nil
}

func (c *QdrantClient) UpsertPoints(ctx context.Context, collectionName string, points []Point) error {
	qdrantPoints := constructPointStruct(points)
	_, err := c.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         qdrantPoints,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert: %w", err)
	}
	return nil
}

func constructPointStruct(points []Point) []*qdrant.PointStruct {
	qdrantPoints := make([]*qdrant.PointStruct, len(points))
	for i, p := range points {
		payload := make(map[string]*qdrant.Value)
		for k, v := range p.Payload {
			payload[k] = qdrant.NewValueString(v)
		}
		qdrantPoints[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDNum(p.ID),
			Vectors: qdrant.NewVectors(p.Vector...),
			Payload: payload,
		}
	}
	return qdrantPoints
}

func (c *QdrantClient) Search(ctx context.Context, collectionName string, vector []float32, topK int) ([]ScoredPoint, error) {
	searchResult, err := c.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(uint64(topK)),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	if searchResult == nil {
		return make([]ScoredPoint, 0), nil
	}
	scored := make([]ScoredPoint, len(searchResult))
	for i, r := range searchResult {
		payload := make(map[string]string)
		for k, v := range r.Payload {
			payload[k] = v.GetStringValue()
		}
		scored[i] = ScoredPoint{
			ID:      r.Id.GetNum(),
			Score:   float64(r.Score),
			Payload: payload,
		}
	}
	return scored, nil
}

func (c *QdrantClient) SearchWithFilter(ctx context.Context, collectionName string, vector []float32, filter string, topK int) ([]ScoredPoint, error) {
	f, err := parseRichFilter(filter)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filter: %w", err)
	}

	searchResult, err := c.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(vector...),
		Filter:         f,
		Limit:          qdrant.PtrOf(uint64(topK)),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}
	if searchResult == nil {
		return make([]ScoredPoint, 0), nil
	}
	scored := make([]ScoredPoint, len(searchResult))
	for i, r := range searchResult {
		payload := make(map[string]string)
		for k, v := range r.Payload {
			payload[k] = v.GetStringValue()
		}
		scored[i] = ScoredPoint{
			ID:      r.Id.GetNum(),
			Score:   float64(r.Score),
			Payload: payload,
		}
	}
	return scored, nil
}

func parseRichFilter(filter string) (*qdrant.Filter, error) {
	filterObj := qdrant.Filter{}
	for _, token := range strings.Split(filter, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		var target *[]*qdrant.Condition
		switch token[0] {
		case '+':
			target = &filterObj.Must
			token = token[1:]
		case '-':
			target = &filterObj.MustNot
			token = token[1:]
		case '|':
			target = &filterObj.Should
			token = token[1:]
		default:
			target = &filterObj.Must
		}
		kv := strings.SplitN(token, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid filter format: %s", token)
		}
		key := strings.TrimSpace(kv[0]) 
		value := strings.TrimSpace(kv[1])
		if strings.Contains(value, "..") {
			bounds := strings.SplitN(value, "..", 2)
			from := strings.TrimSpace(bounds[0])
			to := strings.TrimSpace(bounds[1])
			rng := &qdrant.Range{}
			if from == "" || to == "" {
				return nil, fmt.Errorf("invalid range format: %s", value)
			}
			if strings.Contains(from, "..") || strings.Contains(to, "..") {
				return nil, fmt.Errorf("invalid range format: %s", value)
			}
			fromFloat, err := strconv.ParseFloat(from, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid range format: %s", value)
			}
			toFloat, err := strconv.ParseFloat(to, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid range format: %s", value)
			}
			rng.Gte = qdrant.PtrOf(fromFloat)
			rng.Lte = qdrant.PtrOf(toFloat)
			*target = append(*target, qdrant.NewRange(key, rng))
		} else {
			*target = append(*target, qdrant.NewMatch(key, value))
		}
	}
	return &filterObj, nil
}

func (c *QdrantClient) DeletePoints(ctx context.Context, collectionName string, ids []uint64) error {
	points := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		points[i] = qdrant.NewIDNum(id)
	}
	_, err := c.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points:         qdrant.NewPointsSelector(points...),
	})
	if err != nil {
		return fmt.Errorf("failed to delete points: %w", err)
	}
	return nil
}