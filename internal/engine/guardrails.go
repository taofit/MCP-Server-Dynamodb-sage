package engine

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Guardrail struct {
	protectedTable map[string]bool
	config         *AppConfig
}

const MaxIndividualSize = 400 * 1024
const MaxBatchSize = 16 * 1024 * 1024

func NewGuardrail(config *AppConfig) *Guardrail {
	protectedTable := make(map[string]bool)
	for _, t := range config.ProtectedTables {
		protectedTable[t] = true
	}
	return &Guardrail{
		protectedTable: protectedTable,
		config:         config,
	}
}

func (g *Guardrail) ValidateSchema(tableName string, item map[string]types.AttributeValue) error {
	tableCfg := g.getTableConfig(tableName)
	if tableCfg == nil || !tableCfg.EnforceSchema {
		return nil
	}
	errSlice := []error{}
	for field, value := range item {
		expectedType, exists := tableCfg.Columns[field]
		if !exists {
			continue
		}

		if !g.matchType(value, expectedType) {
			errSlice = append(errSlice, fmt.Errorf("field %s does not match the expected type %s: %v", field, expectedType, value))
		}
	}

	if len(errSlice) > 0 {
		return fmt.Errorf("schema validation failed: %v", errors.Join(errSlice...))
	}
	return nil
}

func (g *Guardrail) matchType(value types.AttributeValue, expected string) bool {
	switch expected {
	case "S":
		_, ok := value.(*types.AttributeValueMemberS)
		return ok
	case "N":
		_, ok := value.(*types.AttributeValueMemberN)
		return ok
	case "B":
		_, ok := value.(*types.AttributeValueMemberB)
		return ok
	case "BOOL":
		_, ok := value.(*types.AttributeValueMemberBOOL)
		return ok
	case "NULL":
		_, ok := value.(*types.AttributeValueMemberNULL)
		return ok
	case "SS":
		_, ok := value.(*types.AttributeValueMemberSS)
		return ok
	case "NS":
		_, ok := value.(*types.AttributeValueMemberNS)
		return ok
	case "BS":
		_, ok := value.(*types.AttributeValueMemberBS)
		return ok
	case "L":
		_, ok := value.(*types.AttributeValueMemberL)
		return ok
	case "M":
		_, ok := value.(*types.AttributeValueMemberM)
		return ok
	}
	return false
}

func (g *Guardrail) getTableConfig(tableName string) *TableConfig {
	for _, table := range g.config.Tables {
		if table.Name == tableName {
			return &table
		}
	}
	return nil
}

func (g *Guardrail) ValidateReadOnlyTable(tableName string) error {
	tableCfg := g.getTableConfig(tableName)
	if tableCfg != nil && tableCfg.ReadOnly {
		return fmt.Errorf("table %s is read-only", tableName)
	}
	return nil
}

func (g *Guardrail) ValidateCapacityUnits(cu int64) error {
	maxThroughputIncrease := int64(g.config.RiskThresholds.MaxThroughputIncrease)
	if cu > maxThroughputIncrease {
		return fmt.Errorf("capacity units %d exceed limit of %d", cu, maxThroughputIncrease)
	}
	return nil
}

func (g *Guardrail) EnforceLimit(limit int32) (int32, string) {
	var warning string
	if limit <= 0 {
		limit = g.config.GlobalLimits.DefaultLimit
	}

	if limit > g.config.GlobalLimits.MaxLimit {
		limit = g.config.GlobalLimits.MaxLimit
		warning = fmt.Sprintf("Limit was set to %d as it was higher than the maximum allowed limit: %d", limit, g.config.GlobalLimits.MaxLimit)
	}
	return limit, warning
}

func (g *Guardrail) ScrubItems(tableName string, items []map[string]any) []map[string]any {
	for _, item := range items {
		for field := range item {
			if g.isSensitiveField(field) || g.isPIIField(tableName, field) {
				item[field] = fmt.Sprintf("%s:[REDACTED]", field)
			}
		}
	}

	return items
}

func (g *Guardrail) isSensitiveField(field string) bool {
	for _, sensitiveField := range g.config.SensitiveFields {
		if strings.EqualFold(field, sensitiveField) {
			return true
		}
	}

	return false
}

func (g *Guardrail) CheckTablePIIFields(tableName string, fields map[string]interface{}) []string {
	sensitiveList := []string{}
	for field := range fields {
		if g.isPIIField(tableName, field) {
			sensitiveList = append(sensitiveList, field)
		}
	}

	return sensitiveList
}

func (g *Guardrail) isPIIField(tableName, field string) bool {
	tableCfg := g.getTableConfig(tableName)
	if tableCfg != nil {
		for _, sensitiveField := range tableCfg.PIIFields {
			if strings.EqualFold(field, sensitiveField) {
				return true
			}
		}
	}

	return false
}

func (g *Guardrail) GetSensitiveFields(items map[string]interface{}) []string {
	sensitiveList := []string{}
	for field := range items {
		if g.isSensitiveField(field) {
			sensitiveList = append(sensitiveList, field)
		}
	}

	return sensitiveList
}

func (g *Guardrail) ValidateProtectedTable(tableName string) error {
	if _, ok := g.protectedTable[tableName]; ok {
		return fmt.Errorf("table %s is protected and cannot be modified", tableName)
	}

	return nil
}

func (g *Guardrail) ValidateBatchSize(writeRequests []types.WriteRequest) error {
	batchSize := 0
	for _, eachRequest := range writeRequests {
		size := 0
		if eachRequest.PutRequest != nil {
			size = g.GetEstimatedSize(eachRequest.PutRequest.Item)
		} else if eachRequest.DeleteRequest != nil {
			size = g.GetEstimatedSize(eachRequest.DeleteRequest.Key)
		}

		if size > MaxIndividualSize {
			return fmt.Errorf("item size exceeds limit of %dKB", MaxIndividualSize/1024)
		}
		batchSize += size
	}

	if batchSize > MaxBatchSize {
		return fmt.Errorf("batch size exceeds limit of %dMB", MaxBatchSize/(1024*1024))
	}

	return nil
}

func (g *Guardrail) GetEstimatedSize(item map[string]types.AttributeValue) int {
	size := 0
	for key, value := range item {
		size += g.calculateSize(key, value)
	}
	return size
}

func (g *Guardrail) GetEstimatedRCU(item map[string]types.AttributeValue, consistent bool) float64 {
	rcu := math.Ceil(float64(g.GetEstimatedSize(item)) / 4096.0)
	if !consistent {
		rcu = rcu * 0.5
	}
	if rcu < 0.5 {
		return 0.5 // minimum is 0.5 RCU for eventually consistent
	}
	return rcu
}

func (g *Guardrail) GetEstimatedWCU(item map[string]types.AttributeValue) float64 {
	wcu := math.Ceil(float64(g.GetEstimatedSize(item)) / 1024.0)
	if wcu < 1.0 {
		return 1.0
	}
	return wcu
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
		size += g.GetEstimatedSize(v.Value)
	}

	return size
}
