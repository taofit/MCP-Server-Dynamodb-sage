package engine

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Guardrail struct {
	maxLimit       int32
	fieldToScrub   []string
	defaultLimit   int32
	protectedTable map[string]bool
}

const MaxIndividualSize = 400 * 1024
const MaxBatchSize = 16 * 1024 * 1024

func NewGuardrail(maxLimit int32, defaultLimit int32) *Guardrail {
	return &Guardrail{
		maxLimit:     maxLimit,
		defaultLimit: defaultLimit,
		fieldToScrub: []string{"password", "ssn", "token", "api_key", "secret", "card_number", "credit_card"},
		protectedTable: map[string]bool{
			"Transactions": true,
			"SystemConfig": true,
		},
	}
}

func (g *Guardrail) EnforceLimit(limit int32) (int32, string) {
	var warning string
	if limit <= 0 {
		limit = g.defaultLimit
	}

	if limit > g.maxLimit {
		limit = g.maxLimit
		warning = fmt.Sprintf("Limit was set to %d as it was higher than the maximum allowed limit: %d", limit, g.maxLimit)
	}
	return limit, warning
}

func (g *Guardrail) ScrubItems(items []map[string]any) []map[string]any {
	for _, item := range items {
		for field := range item {
			if g.isSensitiveField(field) {
				item[field] = fmt.Sprintf("%s:[REDACTED]", field)
			}
		}
	}

	return items
}

func (g *Guardrail) isSensitiveField(field string) bool {
	for _, sensitiveField := range g.fieldToScrub {
		if strings.EqualFold(field, sensitiveField) {
			return true
		}
	}
	return false
}

func (g *Guardrail) ValidateDelete(tableName string) error {
	if _, ok := g.protectedTable[tableName]; ok {
		return fmt.Errorf("Access is denied to table %s", tableName)
	}

	return nil
}

func (g *Guardrail) ValidateBatchSize(writeRequests []types.WriteRequest) error {
	batchSize := 0
	for _, eachRequest := range writeRequests {
		size := 0
		if eachRequest.PutRequest != nil {
			size = g.estimatedSize(eachRequest.PutRequest.Item)
		} else if eachRequest.DeleteRequest != nil {
			size = g.estimatedSize(eachRequest.DeleteRequest.Key)
		}

		if size > MaxIndividualSize {
			return fmt.Errorf("Item size exceeds limit of %dKB", MaxIndividualSize/1024)
		}
		batchSize += size
	}

	if batchSize > MaxBatchSize {
		return fmt.Errorf("Batch size exceeds limit of %dMB", MaxBatchSize/(1024*1024))
	}

	return nil
}

func (g *Guardrail) estimatedSize(item map[string]types.AttributeValue) int {
	size := 0
	for key, value := range item {
		size += g.calculateSize(key, value)
	}

	return size
}

func (g *Guardrail) calculateSize(key string, value types.AttributeValue) int {
	size := len(key)
	switch v := value.(type) {
	case *types.AttributeValueMemberS:
		size += len(v.Value)
	case *types.AttributeValueMemberN:
		size += len(v.Value)
	case *types.AttributeValueMemberB:
		size += len(v.Value)
	case *types.AttributeValueMemberBOOL:
		size += 1
	case *types.AttributeValueMemberNULL:
		size += 1
	case *types.AttributeValueMemberSS:
		// Sum actual string lengths, not just element count
		for _, s := range v.Value {
			size += len(s)
		}
	case *types.AttributeValueMemberNS:
		for _, s := range v.Value {
			size += len(s)
		}
	case *types.AttributeValueMemberBS:
		for _, b := range v.Value {
			size += len(b)
		}
	case *types.AttributeValueMemberL:
		// Recurse into each list element (no key for list elements)
		for _, elem := range v.Value {
			size += g.calculateSize("", elem)
		}
	case *types.AttributeValueMemberM:
		// Recurse into nested map
		size += g.estimatedSize(v.Value)
	}

	return size
}
