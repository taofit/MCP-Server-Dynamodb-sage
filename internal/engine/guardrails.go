package engine

import (
	"fmt"
	"strings"
)

type Guardrail struct {
	maxLimit       int32
	fieldToScrub   []string
	defaultLimit   int32
	protectedTable map[string]bool
}

func NewGuardrail(maxLimit int32, defaultLimit int32) *Guardrail {
	return &Guardrail{
		maxLimit:       maxLimit,
		defaultLimit:   defaultLimit,
		fieldToScrub:   []string{"password", "ssn", "token", "api_key", "secret", "card_number", "credit_card"},
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