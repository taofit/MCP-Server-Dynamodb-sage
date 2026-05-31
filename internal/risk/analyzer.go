package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"dynamodb-sage/internal/engine"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RiskAnalyzer struct {
	thresholds engine.RiskThresholds
	Level      engine.RiskLevel
	db         *dynamodb.Client
}

type Assessment struct {
	Level        engine.RiskLevel `json:"risk_level"`
	EstimatedRCU float64          `json:"estimated_rcu,omitempty"`
	EstimatedWCU float64          `json:"estimated_wcu,omitempty"`
	EstimatedUSD float64          `json:"estimated_usd,omitempty"`
	Reason       string           `json:"reason"`
}

const (
	rcuPageSizeBytes   = 4 * 1024 // 4 KB
	pricePerMillionRCU = 0.125
)

func NewRiskAnalyzer(config *engine.AppConfig, db *dynamodb.Client) *RiskAnalyzer {
	return &RiskAnalyzer{
		thresholds: config.RiskThresholds,
		Level:      engine.LowRiskLevel,
		db:         db,
	}
}

func (ra *RiskAnalyzer) Analyze(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	switch req.Params.Name {
	case "scan_table":
		return ra.analyzeScan(ctx, req)
	case "batch_delete_items":
		return ra.analyzeBatchDelete(ctx, req)
	case "batch_get_items":
		return ra.analyzeBatchGet(ctx, req)
	case "batch_put_items":
		return ra.analyzeBatchPut(ctx, req)
	case "delete_table":
		return ra.analyzeDeleteTable(ctx, req)
	case "delete_item":
		return ra.analyzeDeleteItem(ctx, req)
	case "update_table":
		return ra.analyzeUpdateTable(ctx, req)
	default:
		return Assessment{Level: engine.LowRiskLevel}, nil
	}
}

func (ra *RiskAnalyzer) analyzeScan(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string `json:"tableName"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{
			Level:  engine.MediumRiskLevel,
			Reason: fmt.Sprintf("Failed to parse arguments: %s", err),
		}, nil
	}

	desc, err := ra.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &args.TableName,
	})
	if err != nil {
		return Assessment{
			Level:  engine.MediumRiskLevel,
			Reason: fmt.Sprintf("Failed to describe table: %s", err),
		}, nil
	}

	var reason []string
	size := ra.ToMB(ra.safeInt64(desc.Table.TableSizeBytes))
	count := ra.safeInt64(desc.Table.ItemCount)

	if size > ra.thresholds.TableSizeMB {
		reason = append(reason, fmt.Sprintf("Table size (%.2f MB) exceeds scan threshold with %d items", size, count))
	}

	rcu := math.Ceil(float64(count*ra.safeInt64(desc.Table.TableSizeBytes))/float64(rcuPageSizeBytes)) * 0.5
	cost := float64(rcu) * pricePerMillionRCU / 1_000_000.0

	if ra.thresholds.ScanCostUSD > 0 && cost > ra.thresholds.ScanCostUSD {
		reason = append(reason, fmt.Sprintf("Scan cost ($%.2f) exceeds threshold", cost))
	}

	level := engine.LowRiskLevel
	if len(reason) > 0 {
		level = engine.MediumRiskLevel
	}
	if len(reason) >= 2 {
		level = engine.HighRiskLevel
	}

	return Assessment{
		Level:        level,
		Reason:       strings.Join(reason, "; "),
		EstimatedRCU: rcu,
		EstimatedUSD: cost,
	}, nil
}

func (ra *RiskAnalyzer) analyzeBatchDelete(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	return Assessment{Level: engine.LowRiskLevel}, nil
}

func (ra *RiskAnalyzer) analyzeBatchGet(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableNames []string `json:"tableNames"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, err
	}

	if len(args.TableNames) > ra.thresholds.BatchGetItemCountThreshold {
		return Assessment{
			Level:  engine.HighRiskLevel,
			Reason: fmt.Sprintf("BatchGetItems request for %d tables exceeds threshold", len(args.TableNames)),
		}, nil
	}

	return Assessment{Level: engine.LowRiskLevel}, nil
}

func (ra *RiskAnalyzer) analyzeBatchPut(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	return Assessment{Level: engine.LowRiskLevel}, nil
}

func (ra *RiskAnalyzer) analyzeDeleteTable(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	return Assessment{Level: engine.HighRiskLevel, Reason: "Table deletion is a destructive operation"}, nil
}

func (ra *RiskAnalyzer) analyzeDeleteItem(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	return Assessment{Level: engine.LowRiskLevel}, nil
}

func (ra *RiskAnalyzer) analyzeUpdateTable(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	return Assessment{Level: engine.LowRiskLevel}, nil
}

func (assessment Assessment) IsHighRisk() bool {
	return assessment.Level >= engine.HighRiskLevel
}

func (ra *RiskAnalyzer) ToMB(size int64) float64 {
	return float64(size) / (1024 * 1024)
}

func (ra *RiskAnalyzer) ToKB(size int64) float64 {
	return float64(size) / 1024
}

func (ra *RiskAnalyzer) safeInt64(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}