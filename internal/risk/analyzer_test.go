package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"dynamodb-sage/internal/engine"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// helper to build a large string (size > 400 KB)
func largeString(t *testing.T, size int) string {
	sb := strings.Builder{}
	sb.Grow(size)
	for i := 0; i < size; i++ {
		sb.WriteByte('a')
	}
	return sb.String()
}

// minimal config used by the tests
func testConfig() *engine.AppConfig {
	return &engine.AppConfig{
		GlobalLimits: engine.GlobalLimits{
			MaxLimit:     100,
			DefaultLimit: 20,
		},
		SensitiveFields: []string{},
		ProtectedTables: []string{"ProtectedTable"},
		Tables: []engine.TableConfig{
			{
				Name:          "ReadOnlyTable",
				ReadOnly:      true,
				EnforceSchema: false,
				Columns:       map[string]string{"id": "S"},
			},
			{
				Name:          "ProtectedTable",
				ReadOnly:      false,
				EnforceSchema: false,
				Columns:       map[string]string{"id": "S"},
			},
			{
				Name:          "NormalTable",
				ReadOnly:      false,
				EnforceSchema: false,
				Columns:       map[string]string{"id": "S"},
			},
		},
		RiskThresholds: engine.RiskThresholds{
			TableSizeMB:           10,
			ScanCostUSD:           0.05,
			BatchDeleteCount:      100,
			BatchGetCount:         100,
			BatchPutCount:         100,
			MaxThroughputIncrease: 2000,
			UpdateExpressionDepth: 5,
		},
	}
}

// ---------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------

func TestAnalyzePutItem_ReadOnlyAndProtected(t *testing.T) {
	cfg := testConfig()
	guard := engine.NewGuardrail(cfg)

	ra := NewRiskAnalyzer(cfg, nil, guard)

	// payload that passes schema but triggers table‑protection checks
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "put_item",
			Arguments: json.RawMessage(`{
				"tableName":"ReadOnlyTable",
				"item":{"id":"123"}
			}`),
		},
	}
	assessment, err := ra.analyzePutItem(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Level != engine.HighRiskLevel {
		t.Fatalf("expected HighRisk due to read‑only table, got %v", assessment.Level)
	}
	if !strings.Contains(assessment.Reason, "readonly") {
		t.Fatalf("reason should contain 'readonly': %s", assessment.Reason)
	}

	// protected table
	req.Params.Arguments = json.RawMessage(`{
		"tableName":"ProtectedTable",
		"item":{"id":"123"}
	}`)
	assessment, err = ra.analyzePutItem(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Level != engine.HighRiskLevel {
		t.Fatalf("expected HighRisk due to protected table, got %v", assessment.Level)
	}
	if !strings.Contains(assessment.Reason, "protectedTable") {
		t.Fatalf("reason should contain 'protectedTable': %s", assessment.Reason)
	}
}

func TestAnalyzePutItem_OversizedPayload(t *testing.T) {
	cfg := testConfig()
	guard := engine.NewGuardrail(cfg)
	ra := NewRiskAnalyzer(cfg, nil, guard)

	// Build an item whose estimated size > 400 KB
	largeVal := largeString(t, engine.MaxIndividualSize+10) // 400KB + 10 bytes
	item := map[string]interface{}{
		"id":   "123",
		"blob": largeVal,
	}
	avItem, _ := attributevalue.MarshalMap(item)

	// verify size calculation (sanity check)
	if sz := guard.GetEstimatedSize(avItem); sz <= engine.MaxIndividualSize {
		t.Fatalf("test setup error – generated item size %d is not oversized", sz)
	}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "put_item",
			Arguments: json.RawMessage(fmt.Sprintf(`{
				"tableName":"NormalTable",
				"item":%s
			}`, mustJSON(item))),
		},
	}
	assessment, err := ra.analyzePutItem(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
		t.Fatalf("expected at least MediumRisk for oversized payload, got %v", assessment.Level)
	}
	if !strings.Contains(assessment.Reason, "exceeds DynamoDB 400KB limit") {
		t.Fatalf("reason should mention size limit: %s", assessment.Reason)
	}
}

func TestAnalyzeBatchPut_ExceedBatchCount(t *testing.T) {
	cfg := testConfig()
	guard := engine.NewGuardrail(cfg)
	ra := NewRiskAnalyzer(cfg, nil, guard)

	// Build 101 items – config BatchPutCount = 100
	items := make([]map[string]interface{}, 0, 101)
	for i := 0; i < 101; i++ {
		items = append(items, map[string]interface{}{
			"id": fmt.Sprintf("item-%d", i),
		})
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "batch_put_items",
			Arguments: json.RawMessage(fmt.Sprintf(`{
				"tableName":"NormalTable",
				"items":%s
			}`, mustJSON(items))),
		},
	}
	assessment, err := ra.analyzeBatchPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
		t.Fatalf("expected at least MediumRisk for batch‑count overflow, got %v", assessment.Level)
	}
	if !strings.Contains(assessment.Reason, "Batch put of 101 items exceeds") {
		t.Fatalf("reason should mention batch‑put count overflow: %s", assessment.Reason)
	}
}

func TestAnalyzeUpdateItem_ComplexExpression(t *testing.T) {
	cfg := testConfig()
	guard := engine.NewGuardrail(cfg)
	ra := NewRiskAnalyzer(cfg, nil, guard)

	// expression with more actions than the allowed depth (5)
	updateExp := "SET #a = :a, #b = :b, #c = :c, #d = :d, #e = :e, #f = :f"
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "update_item",
			Arguments: json.RawMessage(fmt.Sprintf(`{
				"tableName":"NormalTable",
				"key":{"id":"123"},
				"updateExpression":"%s",
				"expressionAttributeValues":{}
			}`, updateExp)),
		},
	}
	assessment, err := ra.analyzeUpdateItem(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
		t.Fatalf("expected at least MediumRisk for complex update expression, got %v", assessment.Level)
	}
	if !strings.Contains(assessment.Reason, "Too many actions") {
		t.Fatalf("reason should mention expression complexity: %s", assessment.Reason)
	}
}

// ---------------------------------------------------------------------
// Utility helpers used by the tests
// ---------------------------------------------------------------------

// mustJSON marshals v to JSON and panics on error – convenient for test literals.
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json marshal error: %v", err))
	}
	return string(b)
}

// ---------------------------------------------------------------------
// Additional table‑driven tests for other analyze methods
// ---------------------------------------------------------------------

func TestAnalyzeScan_ReadOnlyTable(t *testing.T) {
    cfg := testConfig()
    guard := engine.NewGuardrail(cfg)
    ra := NewRiskAnalyzer(cfg, nil, guard)

    req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "scan_table", Arguments: json.RawMessage(`{"tableName":"ReadOnlyTable"}`)}}
    assessment, err := ra.analyzeScan(context.Background(), req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // Scan should be allowed; risk level low unless size exceeds thresholds
    if assessment.Level != engine.LowRiskLevel && assessment.Level != engine.MediumRiskLevel {
        t.Fatalf("expected Low or Medium risk for scan on read‑only table, got %v", assessment.Level)
    }
}

func TestAnalyzeBatchDelete_ExceedBatchSize(t *testing.T) {
    cfg := testConfig()
    // set a low batch delete threshold to trigger the guardrail
    cfg.RiskThresholds.BatchDeleteCount = 2
    guard := engine.NewGuardrail(cfg)
    ra := NewRiskAnalyzer(cfg, nil, guard)

    keys := []map[string]interface{}{{"id": "1"}, {"id": "2"}, {"id": "3"}}
    args := fmt.Sprintf(`{"tableName":"NormalTable","keys":%s}`, mustJSON(keys))
    req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "batch_delete_items", Arguments: json.RawMessage(args)}}
    assessment, err := ra.analyzeBatchDelete(context.Background(), req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
        t.Fatalf("expected at least MediumRisk for batch delete size overflow, got %v", assessment.Level)
    }
    if !strings.Contains(assessment.Reason, "Batch delete of 3 items exceeds") {
        t.Fatalf("reason should mention batch‑delete count overflow: %s", assessment.Reason)
    }
}

func TestAnalyzeBatchGet_ExceedBatchCount(t *testing.T) {
    cfg := testConfig()
    cfg.RiskThresholds.BatchGetCount = 2
    guard := engine.NewGuardrail(cfg)
    ra := NewRiskAnalyzer(cfg, nil, guard)

    keys := []map[string]interface{}{{"id": "1"}, {"id": "2"}, {"id": "3"}}
    args := fmt.Sprintf(`{"tableName":"NormalTable","keys":%s}`, mustJSON(keys))
    req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "batch_get_items", Arguments: json.RawMessage(args)}}
    assessment, err := ra.analyzeBatchGet(context.Background(), req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
        t.Fatalf("expected at least MediumRisk for batch get count overflow, got %v", assessment.Level)
    }
    if !strings.Contains(assessment.Reason, "BatchGetItems request for 3 items exceeds") {
        t.Fatalf("reason should mention batch‑get count overflow: %s", assessment.Reason)
    }
}

func TestAnalyzeDelete_ProtectedTable(t *testing.T) {
    cfg := testConfig()
    guard := engine.NewGuardrail(cfg)
    ra := NewRiskAnalyzer(cfg, nil, guard)

    req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "delete_item", Arguments: json.RawMessage(`{"tableName":"ProtectedTable"}`)}}
    assessment, err := ra.analyzeDelete(context.Background(), req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
        t.Fatalf("expected at least MediumRisk for delete on protected table, got %v", assessment.Level)
    }
    if !strings.Contains(assessment.Reason, "protectedTable") {
        t.Fatalf("reason should contain 'protectedTable': %s", assessment.Reason)
    }
}

func TestAnalyzeUpdateTable_ReadOnlyTable(t *testing.T) {
    cfg := testConfig()
    guard := engine.NewGuardrail(cfg)
    ra := NewRiskAnalyzer(cfg, nil, guard)

    args := json.RawMessage(`{"tableName":"ReadOnlyTable","provisionedThroughput":{"readCapacity":5}}`)
    req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "update_table", Arguments: args}}
    assessment, err := ra.analyzeUpdateTable(context.Background(), req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if assessment.Level != engine.MediumRiskLevel && assessment.Level != engine.HighRiskLevel {
        t.Fatalf("expected at least MediumRisk for update_table on read‑only table, got %v", assessment.Level)
    }
    if !strings.Contains(assessment.Reason, "readonly") {
        t.Fatalf("reason should contain 'readonly': %s", assessment.Reason)
    }
}

