// Package risk provides risk analysis capabilities for DynamoDB operations.
package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"dynamodb-sage/internal/engine"
	"dynamodb-sage/internal/metrics"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DynamoDBClient interface {
	DescribeTable(ctx context.Context, input *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	GetItem(ctx context.Context, input *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	BatchGetItem(ctx context.Context, input *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
	DescribeTimeToLive(ctx context.Context, input *dynamodb.DescribeTimeToLiveInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTimeToLiveOutput, error)
}

// DynamoAdapter wraps the concrete DynamoDB client and satisfies DynamoDBClient.
// Additional DynamoDB methods can be added here without changing the interface contract.
type DynamoAdapter struct {
	*dynamodb.Client
}

func (a *DynamoAdapter) DescribeTable(ctx context.Context, in *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return a.Client.DescribeTable(ctx, in, optFns...)
}
func (a *DynamoAdapter) GetItem(ctx context.Context, in *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return a.Client.GetItem(ctx, in, optFns...)
}
func (a *DynamoAdapter) BatchGetItem(ctx context.Context, in *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	return a.Client.BatchGetItem(ctx, in, optFns...)
}
func (a *DynamoAdapter) DescribeTimeToLive(ctx context.Context, in *dynamodb.DescribeTimeToLiveInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTimeToLiveOutput, error) {
	return a.Client.DescribeTimeToLive(ctx, in, optFns...)
}
//RiskAnalyzer implements the MCP risk-analysis tool
type RiskAnalyzer struct {
	config      *engine.AppConfig
	db          DynamoDBClient
	guardrail   *engine.Guardrail
	scanHistory sync.Map
}
//Assessment collects all risk parameters
type Assessment struct {
	TableName    string           `json:"table_name"`
	Level        engine.RiskLevel `json:"risk_level"`
	EstimatedRCU float64          `json:"estimated_rcu,omitempty"`
	EstimatedWCU float64          `json:"estimated_wcu,omitempty"`
	EstimatedUSD float64          `json:"estimated_usd,omitempty"`
	Reason       string           `json:"reason"`
}

const (
	rcuPageSizeBytes   = 4 * 1024 // 4 KB
	wcuPageSizeBytes   = 1 * 1024 // 1 KB
	pricePerMillionRCU = 0.125    // Price for eventually consistent reads
)

var patternMatch = map[string]string{
	"name": "S", "email": "S", "phone": "S", "address": "S", "timestamp": "N", "year": "N", "age": "N", "salary": "N", "balance": "N",
}
var hotPartitionKeys = map[string]bool{
	"status": true, "state": true, "gender": true, "active": true, "verified": true, "month": true, "year": true, "enabled": true, "disabled": true, "deleted": true, "archived": true, "ready": true, "processing": true, "unverified": true, "locked": true, "unlocked": true, "open": true, "closed": true, "cancelled": true, "completed": true, "failed": true, "pending": true,
}

func NewRiskAnalyzer(config *engine.AppConfig, db DynamoDBClient, guardrail *engine.Guardrail) *RiskAnalyzer {
	return &RiskAnalyzer{
		config:      config,
		db:          db,
		guardrail:   guardrail,
		scanHistory: sync.Map{},
	}
}

func (ra *RiskAnalyzer) Analyze(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var assessment Assessment
	var err error
	start := time.Now()

	switch req.Params.Name {
	case "scan_table":
		assessment, err = ra.analyzeScan(ctx, req)
	case "batch_delete_items":
		assessment, err = ra.analyzeBatchDelete(ctx, req)
	case "batch_get_items":
		assessment, err = ra.analyzeBatchGet(ctx, req)
	case "batch_put_items":
		assessment, err = ra.analyzeBatchPut(ctx, req)
	case "delete_item", "delete_table":
		assessment, err = ra.analyzeDelete(ctx, req)
	case "update_table":
		assessment, err = ra.analyzeUpdateTable(ctx, req)
	case "put_item":
		assessment, err = ra.analyzePutItem(ctx, req)
	case "update_item":
		assessment, err = ra.analyzeUpdateItem(ctx, req)
	case "create_optimized_table":
		assessment, err = ra.analyzeCreateOptimizedTable(ctx, req)
	default:
		assessment, err = Assessment{Level: engine.LowRiskLevel}, nil
	}
	if err != nil {
		return Assessment{}, err
	}
	instrumentRiskAnalysis(start, req, assessment, ra)

	return assessment, nil
}

func instrumentRiskAnalysis(start time.Time, req *mcp.CallToolRequest, assessment Assessment, ra *RiskAnalyzer) {
	elapsed := time.Since(start).Seconds()
	metrics.RiskAnalysisTotal.WithLabelValues(req.Params.Name, assessment.TableName, ra.String(assessment.Level)).Inc()
	metrics.RiskAnalysisDurationSeconds.WithLabelValues(req.Params.Name).Observe(elapsed)
}

func (ra *RiskAnalyzer) analyzeCreateOptimizedTable(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName            string                   `json:"tableName"`
		KeySchema            []map[string]interface{} `json:"keySchema"`
		AttributeDefinitions []map[string]interface{} `json:"attributeDefinitions"`
		Gsis                 []map[string]interface{} `json:"gsis"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeCreateOptimizedTable called for table %s", args.TableName)
	var reason []string
	reason = append(reason, ra.validateKeySchema(args.KeySchema)...)
	reason = append(reason, ra.validateAttributeDefinitions(args.AttributeDefinitions)...)
	reason = append(reason, ra.validateGsis(args.Gsis)...)

	level := ra.getRiskLevel(reason)
	return Assessment{TableName: args.TableName, Level: level, Reason: strings.Join(reason, "; ")}, nil
}

func (ra *RiskAnalyzer) analyzeUpdateItem(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName                 string                 `json:"tableName"`
		Key                       map[string]interface{} `json:"key"`
		UpdateExpression          string                 `json:"updateExpression"`
		ExpressionAttributeNames  map[string]string      `json:"expressionAttributeNames"`
		ExpressionAttributeValues map[string]interface{} `json:"expressionAttributeValues"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeUpdateItem called for table %s", args.TableName)
	avPayload, err := attributevalue.MarshalMap(args.ExpressionAttributeValues)
	if err != nil {
		return Assessment{}, fmt.Errorf("failed to marshal item: %s", err)
	}

	var reason []string
	if ra.checkTableProtection(args.TableName) != nil {
		reason = append(reason, ra.checkTableProtection(args.TableName)...)
	}
	if err := ra.guardrail.ValidateSchema(args.TableName, avPayload); err != nil {
		reason = append(reason, fmt.Sprintf("Schema validation failed: %s", err.Error()))
	}
	if piif := ra.guardrail.CheckTablePIIFields(args.TableName, args.ExpressionAttributeValues); len(piif) > 0 {
		reason = append(reason, fmt.Sprintf("Update item contains PII_fields %s", strings.Join(piif, "; ")))
	}
	if sf := ra.guardrail.GetSensitiveFields(args.ExpressionAttributeValues); len(sf) > 0 {
		reason = append(reason, fmt.Sprintf("Update item contains sensitive_fields %s", strings.Join(sf, "; ")))
	}

	reasonStr, err := ra.checkCurrentItemPIIFields(ctx, args.TableName, args.Key)
	if err != nil {
		return Assessment{}, err
	}
	if reasonStr != "" {
		reason = append(reason, reasonStr)
	}

	if ra.guardrail.GetEstimatedSize(avPayload) > engine.MaxIndividualSize {
		reason = append(reason, fmt.Sprintf("Updated payload size (%d bytes) exceeds DynamoDB 400KB limit", ra.guardrail.GetEstimatedSize(avPayload)))
	}
	// Note: DynamoDB bills based on the *final* item size after the update. Accurate full‑item size estimation would require fetching the existing item, which is not performed here.
	reasonUpdatedSize, err := ra.checkUpdatedItemSize(ctx, args.TableName, avPayload, args.Key)
	if err != nil {
		return Assessment{}, fmt.Errorf("failed to check updated item size: %s", err)
	}
	if reasonUpdatedSize != "" {
		reason = append(reason, reasonUpdatedSize)
	}

	var updateExp = args.UpdateExpression

	reasonStr = ra.checkExpressionComplexity(updateExp)
	if reasonStr != "" {
		reason = append(reason, reasonStr)
	}

	// Calculate the estimated RCUs and WCUs for the update operation

	level := ra.escalatedRiskLevel(reason)

	return Assessment{
		TableName: args.TableName,
		Level:     level,
		Reason:    strings.Join(reason, "; "),
	}, nil
}

func (ra *RiskAnalyzer) analyzeScan(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string `json:"tableName"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeScan called for table %s", args.TableName)

	var desc *dynamodb.DescribeTableOutput
	var err error
	if ra.db != nil {
		desc, err = ra.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &args.TableName})
		if err != nil {
			return Assessment{}, fmt.Errorf("failed to describe table: %s", err)
		}
	}

	var reason []string
	if desc != nil {
		currentSizeMB := ra.ToMB(ra.safeInt64(desc.Table.TableSizeBytes))
		itemCount := ra.safeInt64(desc.Table.ItemCount)
		if currentSizeMB > ra.config.RiskThresholds.TableSizeMB {
			reason = append(reason, fmt.Sprintf("Table size (%.2f MB) exceeds scan threshold with %d items", currentSizeMB, itemCount))
		}
		rcu, cost := ra.EstimatedReadCost(desc.Table)
		if ra.config.RiskThresholds.ScanCostUSD > 0 && cost > ra.config.RiskThresholds.ScanCostUSD {
			reason = append(reason, fmt.Sprintf("Scan cost ($%.2f) exceeds threshold: %.2f", cost, ra.config.RiskThresholds.ScanCostUSD))
		}
		level := ra.getRiskLevel(reason)
		return Assessment{TableName: args.TableName, Level: level, Reason: strings.Join(reason, "; "), EstimatedRCU: rcu, EstimatedUSD: cost}, nil
	}

	// If DB not available, return low risk with no cost
	level := ra.getRiskLevel(reason)
	return Assessment{TableName: args.TableName, Level: level, Reason: strings.Join(reason, "; ")}, nil
}

func (ra *RiskAnalyzer) analyzeBatchDelete(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string                   `json:"tableName"`
		Keys      []map[string]interface{} `json:"keys"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeBatchDelete called for table %s, %d keys", args.TableName, len(args.Keys))

	var reason []string

	if ra.checkTableProtection(args.TableName) != nil {
		reason = append(reason, ra.checkTableProtection(args.TableName)...)
	}
	if len(args.Keys) > int(ra.config.RiskThresholds.BatchDeleteCount) {
		reason = append(reason, fmt.Sprintf("Batch delete of %d items exceeds threshold of %d. Consider splitting into multiple	batches.", len(args.Keys), ra.config.RiskThresholds.BatchDeleteCount))
	}
	reasonStr, err := ra.checkDeleteItemSize(ctx, args.TableName, args.Keys)
	if err != nil {
		return Assessment{}, err
	}
	reason = append(reason, reasonStr)

	return Assessment{
		TableName: args.TableName,
		Level:     ra.escalatedRiskLevel(reason),
		Reason:    strings.Join(reason, "; "),
	}, nil
}

func (ra *RiskAnalyzer) analyzeBatchGet(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string                   `json:"tableName"`
		Keys      []map[string]interface{} `json:"keys"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeBatchGet called for table %s, %d keys", args.TableName, len(args.Keys))

	var avItems []map[string]types.AttributeValue
	if len(args.Keys) > 0 {
		for _, item := range args.Keys {
			av, err := attributevalue.MarshalMap(item)
			if err != nil {
				return Assessment{}, fmt.Errorf("failed to marshal item: %s", err)
			}
			avItems = append(avItems, av)
		}
	}

	var reason []string
	if len(args.Keys) > int(ra.config.RiskThresholds.BatchGetCount) {
		reason = append(reason, fmt.Sprintf("BatchGetItems request for %d items exceeds threshold", len(args.Keys)))
	}
	if len(avItems) > 0 {
		size := 0
		var rcu float64
		for _, avItem := range avItems {
			size += ra.guardrail.GetEstimatedSize(avItem)
		}
		if size > engine.MaxBatchSize {
			reason = append(reason, fmt.Sprintf("BatchGetItems request size (%d bytes) exceeds threshold (%f MB)", size, ra.ToMB(engine.MaxBatchSize)))
		}

		for _, avItem := range avItems {
			rcu += ra.guardrail.GetEstimatedRCU(avItem, false)
		}
		if reasonStr := ra.validateCapacity(rcu); reasonStr != "" {
			reason = append(reason, reasonStr)
		}
	}
	level := ra.getRiskLevel(reason)

	return Assessment{TableName: args.TableName, Level: level, Reason: strings.Join(reason, "; ")}, nil
}

func (ra *RiskAnalyzer) analyzeBatchPut(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string                   `json:"tableName"`
		Items     []map[string]interface{} `json:"items"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeBatchPut called for table %s, %d items", args.TableName, len(args.Items))

	if len(args.Items) == 0 {
		return Assessment{
			Level:  engine.MediumRiskLevel,
			Reason: fmt.Sprintf("No items to put into table %s", args.TableName),
		}, nil
	}

	var avItems []map[string]types.AttributeValue
	for _, item := range args.Items {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			return Assessment{}, fmt.Errorf("failed to marshal item: %s", err)
		}
		avItems = append(avItems, av)
	}
	// 1. Check if table is read-only
	var reason []string
	reason = append(reason, ra.checkTableProtection(args.TableName)...)

	// 3. Schema validation
	if len(avItems) > 0 {
		for _, avItem := range avItems {
			if err := ra.guardrail.ValidateSchema(args.TableName, avItem); err != nil {
				reason = append(reason, fmt.Sprintf("Schema validation failed: %s", err.Error()))
				break
			}
		}
	}

	// 4. Check for sensitive fields in item
	if len(args.Items) > 0 {
		var sensitiveFields []string
		for _, item := range args.Items {
			sensitiveFields = append(sensitiveFields, ra.guardrail.GetSensitiveFields(item)...)
		}
		if len(sensitiveFields) > 0 {
			reason = append(reason, fmt.Sprintf("Item contains sensitive fields: %s, please consider encryption or tokenization the fields before upload", strings.Join(sensitiveFields, ", ")))
		}
	}
	// 5. Check for PII fields in the table
	if len(args.Items) > 0 {
		var tablePIIFields []string

		for _, item := range args.Items {
			tablePIIFields = append(tablePIIFields, ra.guardrail.CheckTablePIIFields(args.TableName, item)...)
		}
		if len(tablePIIFields) > 0 {
			reason = append(reason, fmt.Sprintf("Item contains table-specific PII fields: %s", strings.Join(tablePIIFields, ", ")))
		}
	}
	// 6. Check item size
	if len(avItems) > 0 {
		for _, avItem := range avItems {
			itemSize := ra.guardrail.GetEstimatedSize(avItem)
			if itemSize > engine.MaxIndividualSize {
				reason = append(reason, fmt.Sprintf("Item size (%d bytes) exceeds DynamoDB 400KB limit", itemSize))
				break
			}
		}
		var wcu float64
		for _, avItem := range avItems {
			wcu += ra.guardrail.GetEstimatedWCU(avItem)
		}
		if reasonStr := ra.validateCapacity(wcu); reasonStr != "" {
			reason = append(reason, reasonStr)
		}
	}
	// 7. Check batch size
	if len(args.Items) > int(ra.config.RiskThresholds.BatchPutCount) { // DynamoDB batch write limit
		reason = append(reason, fmt.Sprintf("Batch put of %d items exceeds DynamoDB limit of %d per request", len(args.Items), ra.config.RiskThresholds.BatchPutCount))
	}

	return Assessment{
		TableName: args.TableName,
		Level:     ra.escalatedRiskLevel(reason),
		Reason:    strings.Join(reason, "; "),
	}, nil
}

func (ra *RiskAnalyzer) analyzeDelete(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string `json:"tableName"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeDelete called for table %s", args.TableName)
	var reason []string
	reason = append(reason, ra.checkTableProtection(args.TableName)...)

	return Assessment{TableName: args.TableName, Level: ra.escalatedRiskLevel(reason), Reason: strings.Join(reason, "; ")}, nil
}

func (ra *RiskAnalyzer) analyzeUpdateTable(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName             string                 `json:"tableName"`
		ProvisionedThroughput map[string]interface{} `json:"provisionedThroughput"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzeUpdateTable called for table %s", args.TableName)

	var reason []string
	var desc *dynamodb.DescribeTableOutput
	if ra.db != nil {
		desc, _ = ra.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &args.TableName})
	}

	if len(args.ProvisionedThroughput) > 0 {
		// float64 is used, as Unmarshal stores float64 for JSON numbers in the interface value?
		if rcu, ok := args.ProvisionedThroughput["readCapacity"].(float64); ok {
			if reasonStr := ra.validateCapacity(rcu); reasonStr != "" {
				reason = append(reason, reasonStr)
			}
			if desc != nil && desc.Table.ProvisionedThroughput != nil && rcu < float64(*desc.Table.ProvisionedThroughput.ReadCapacityUnits) {
				log.Printf("Warning: Reducing ReadCapacityUnits for table %s from %d to %f", args.TableName, *desc.Table.ProvisionedThroughput.ReadCapacityUnits, rcu)
			}
		}
		if wcu, ok := args.ProvisionedThroughput["writeCapacity"].(float64); ok {
			if reasonStr := ra.validateCapacity(wcu); reasonStr != "" {
				reason = append(reason, reasonStr)
			}
			if desc != nil && desc.Table.ProvisionedThroughput != nil && wcu < float64(*desc.Table.ProvisionedThroughput.WriteCapacityUnits) {
				log.Printf("Warning: Reducing WriteCapacityUnits for table %s from %d to %f", args.TableName, *desc.Table.ProvisionedThroughput.WriteCapacityUnits, wcu)
			}
		}
	}

	reason = append(reason, ra.checkTableProtection(args.TableName)...)

	return Assessment{
		TableName: args.TableName,
		Level:     ra.escalatedRiskLevel(reason),
		Reason:    strings.Join(reason, "; "),
	}, nil
}

func (ra *RiskAnalyzer) analyzePutItem(ctx context.Context, req *mcp.CallToolRequest) (Assessment, error) {
	var args struct {
		TableName string                 `json:"tableName"`
		Item      map[string]interface{} `json:"item"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return Assessment{}, fmt.Errorf("failed to parse arguments: %s", err)
	}
	log.Printf("[RiskAnalyzer] analyzePutItem called for table %s", args.TableName)

	if len(args.Item) == 0 {
		return Assessment{
			Level:  engine.MediumRiskLevel,
			Reason: fmt.Sprintf("Item to put into table %s cannot be empty", args.TableName),
		}, nil
	}

	avItem, err := attributevalue.MarshalMap(args.Item)
	if err != nil {
		return Assessment{
			Level:  engine.MediumRiskLevel,
			Reason: fmt.Sprintf("Failed to marshal item: %s", err),
		}, nil
	}

	var reason []string
	reason = append(reason, ra.checkTableProtection(args.TableName)...)

	// 2. Schema validation
	if err := ra.guardrail.ValidateSchema(args.TableName, avItem); err != nil {
		reason = append(reason, fmt.Sprintf("Schema validation failed: %s", err.Error()))
	}

	// 3. Check for sensitive fields in item
	sensitiveFields := ra.guardrail.GetSensitiveFields(args.Item)
	if len(sensitiveFields) > 0 {
		reason = append(reason, fmt.Sprintf("Item contains sensitive fields: %s, please consider encryption or tokenization", strings.Join(sensitiveFields, ", ")))
	}

	// 4. Check table-specific PII fields from config
	tablePIIFields := ra.guardrail.CheckTablePIIFields(args.TableName, args.Item)
	if len(tablePIIFields) > 0 {
		reason = append(reason, fmt.Sprintf("Item contains table-specific PII fields: %s", strings.Join(tablePIIFields, ", ")))
	}

	// 5. Check item size
	itemSize := ra.guardrail.GetEstimatedSize(avItem)
	if itemSize > engine.MaxIndividualSize {
		reason = append(reason, fmt.Sprintf("Item size (%d bytes) exceeds DynamoDB 400KB limit", itemSize))
	}

	return Assessment{
		TableName: args.TableName,
		Level:     ra.escalatedRiskLevel(reason),
		Reason:    strings.Join(reason, "; "),
	}, nil
}

func (ra *RiskAnalyzer) checkTableProtection(tableName string) []string {
	var reason []string
	if err := ra.guardrail.ValidateReadOnlyTable(tableName); err != nil {
		reason = append(reason, ra.protectionError("readonly", err))
	}
	if err := ra.guardrail.ValidateProtectedTable(tableName); err != nil {
		reason = append(reason, ra.protectionError("protectedTable", err))
	}
	return reason
}

func (ra *RiskAnalyzer) protectionError(prefix string, err error) string {
	return fmt.Sprintf("%s table access blocked: %s", prefix, err.Error())
}

func (assessment Assessment) IsHighRisk() bool {
	return assessment.Level >= engine.HighRiskLevel
}
func (assessment Assessment) IsLowRisk() bool {
	return assessment.Level == engine.LowRiskLevel
}
func (assessment Assessment) IsMediumRisk() bool {
	return assessment.Level == engine.MediumRiskLevel
}
func (assessment Assessment) IsRisk() bool {
	return assessment.Level >= engine.MediumRiskLevel
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

func (ra *RiskAnalyzer) EstimatedReadCost(tableDescription *types.TableDescription) (float64, float64) {
	rcu := math.Ceil(float64(ra.safeInt64(tableDescription.TableSizeBytes))/float64(rcuPageSizeBytes)) * 0.5
	if tableDescription.BillingModeSummary != nil && tableDescription.BillingModeSummary.BillingMode == types.BillingModeProvisioned {
		return rcu, 0
	}

	return rcu, float64(rcu) * pricePerMillionRCU / 1_000_000.0
}

func (ra *RiskAnalyzer) checkUpdatedItemSize(ctx context.Context, tableName string, avPayload map[string]types.AttributeValue, key map[string]interface{}) (string, error) {
	avKey, err := attributevalue.MarshalMap(key)
	if err != nil {
		return "", fmt.Errorf("failed to marshal key: %s", err)
	}
	payloadSize := ra.guardrail.GetEstimatedSize(avPayload)
	if payloadSize > engine.MaxIndividualSize {
		return fmt.Sprintf("Payload size (%d bytes) exceeds DynamoDB 400KB limit", payloadSize), nil
	}
	output, err := ra.getItemFromDB(ctx, &dynamodb.GetItemInput{TableName: &tableName, Key: avKey})
	if err != nil {
		return "", fmt.Errorf("failed to get item: %s", err)
	}

	if output.Item == nil {
		return "", nil
	}
	currentItemSize := ra.guardrail.GetEstimatedSize(output.Item)
	updatedItemSize := currentItemSize + payloadSize // adds payload size to current item size.
	if updatedItemSize > engine.MaxIndividualSize {
		return fmt.Sprintf("Updated item size (%d bytes) exceeds DynamoDB 400KB limit", updatedItemSize), nil
	}
	return "", nil
}

func (ra *RiskAnalyzer) checkExpressionComplexity(updateExp string) string {
	updateExp = strings.TrimSpace(updateExp)
	if updateExp == "" {
		return "update expression is empty"
	}
	clauseCount := 0
	UpdateExpressionClause := [4]string{"SET", "ADD", "REMOVE", "DELETE"}
	for _, clause := range UpdateExpressionClause {
		if strings.HasPrefix(updateExp, clause) || strings.Contains(updateExp, " "+clause) {
			clauseCount++
		}
	}
	if clauseCount == 0 {
		return "Invalid update expression"
	}
	reason := ""
	actionList := strings.Split(updateExp, ",")
	actionCount := len(actionList)
	actionCount += clauseCount - 1

	if actionCount > int(ra.config.RiskThresholds.UpdateExpressionDepth) {
		reason = "Too many actions in the expression"
	}

	return reason
}

func (ra *RiskAnalyzer) checkDeleteItemSize(ctx context.Context, tableName string, keys []map[string]interface{}) (string, error) {
	avKeys := make([]map[string]types.AttributeValue, 0, len(keys))
	for _, key := range keys {
		avKey, err := attributevalue.MarshalMap(key)
		if err != nil {
			return "", fmt.Errorf("failed to marshal key: %s", err)
		}
		avKeys = append(avKeys, avKey)
	}
	totalSize, err := ra.getBatchFromDB(ctx, tableName, avKeys)
	if err != nil {
		return "", err
	}
	if totalSize > engine.MaxBatchSize {
		return fmt.Sprintf("Total size of items to be deleted (%d bytes) exceeds DynamoDB batch-delete limit(%d)", totalSize, engine.MaxBatchSize), nil
	}
	return "", nil
}

func (ra *RiskAnalyzer) checkCurrentItemPIIFields(ctx context.Context, tableName string, key map[string]interface{}) (string, error) {
	avKey, err := attributevalue.MarshalMap(key)
	if err != nil {
		return "", fmt.Errorf("failed to marshal key: %s", err)
	}
	params := &dynamodb.GetItemInput{
		TableName: &tableName,
		Key:       avKey,
	}
	output, err := ra.getItemFromDB(ctx, params)
	if err != nil {
		return "", err
	}
	if output.Item == nil {
		return "", nil
	}
	var itemResponse map[string]interface{}
	if err := attributevalue.UnmarshalMap(output.Item, &itemResponse); err != nil {
		return "", err
	}
	if piif := ra.guardrail.CheckTablePIIFields(tableName, itemResponse); len(piif) > 0 {
		return fmt.Sprintf("current item contains sensitive_fields %s", strings.Join(piif, "; ")), nil
	}
	return "", nil
}

func (ra *RiskAnalyzer) validateCapacity(cu float64) string {
	if err := ra.guardrail.ValidateCapacityUnits(int64(cu)); err != nil {
		return fmt.Sprintf("Total CapacityUnits %f for the batch: %s", cu, err)
	}
	return ""
}

func (ra *RiskAnalyzer) validateKeySchema(keySchema []map[string]interface{}) []string {
	var reason []string
	for _, avKey := range keySchema {
		attributeName, hasAN := avKey["attributeName"].(string)
		keyType, hasKT := avKey["keyType"].(string)
		if hasKT && hasAN {
			if keyType == string(types.KeyTypeHash) {
				if hotPartitionKeys[strings.ToLower(attributeName)] {
					reason = append(reason, fmt.Sprintf("Hash key \"%s\" is a known hot partition key", attributeName))
				}
			}
		}
	}

	var hasSortKey bool
	var hasHashKey bool
	for _, avKey := range keySchema {
		keyType, _ := avKey["keyType"].(string)
		if keyType == string(types.KeyTypeHash) {
			hasHashKey = true
		}
		if keyType == string(types.KeyTypeRange) {
			hasSortKey = true
		}
	}
	if !hasHashKey {
		reason = append(reason, "Table does not have a hash key, which is required for DynamoDB tables")
	}
	if hasHashKey && !hasSortKey {
		reason = append(reason, "Table does not have a sort key, which is recommended for better query performance and data organization")
	}
	return reason
}

func (ra *RiskAnalyzer) validateAttributeDefinitions(attributeDefinitions []map[string]interface{}) []string {
	var reason []string
	for _, avItem := range attributeDefinitions {
		attributeName, hasAN := avItem["attributeName"].(string)
		attributeType, hasAT := avItem["attributeType"].(string)
		if hasAN && hasAT {
			// Check if the attribute name exists in patternMatch before comparing types
			if expectedType, exists := patternMatch[attributeName]; exists && expectedType != attributeType {
				reason = append(reason, fmt.Sprintf("Attribute \"%s\" type %s doesn't match expected type %s", attributeName, attributeType, expectedType))
			}
			if len(attributeName) > 100 {
				reason = append(reason, fmt.Sprintf("Attribute name \"%s\" exceeds 100 characters limit", attributeName))
			}
		}
	}
	return reason
}

func (ra *RiskAnalyzer) validateGsis(gsis []map[string]interface{}) []string {
	var reason []string
	for _, gsis := range gsis {
		partitionKey, hasPartitionKey := gsis["partitionKey"].(string)
		if !hasPartitionKey {
			continue
		}
		if hotPartitionKeys[strings.ToLower(partitionKey)] {
			reason = append(reason, fmt.Sprintf("Global Secondary Index \"%s\" is a known hot partition key", partitionKey))
		}

		projectionType, hasPrjt := gsis["projectionType"].(string)
		if hasPrjt && projectionType == string(types.ProjectionTypeAll) {
			reason = append(reason, fmt.Sprintf("GSI %s uses ProjectionType %s", partitionKey, projectionType))
		}
	}
	if len(gsis) > 5 {
		reason = append(reason, fmt.Sprintf("Table has %d GSIs, exceeding the recommended maximum of 5", len(gsis)))
	}
	return reason
}

func (ra *RiskAnalyzer) getItemFromDB(ctx context.Context, getItemInput *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
	if ra.db == nil {
		return &dynamodb.GetItemOutput{}, nil
	}
	return ra.db.GetItem(ctx, getItemInput)
}

func (ra *RiskAnalyzer) getBatchFromDB(ctx context.Context, tableName string, avKeys []map[string]types.AttributeValue) (int, error) {
	if ra.db == nil {
		return 0, nil
	}

	totalSize := 0
	batchGetCount := int(ra.config.RiskThresholds.BatchGetCount)
	for i := 0; i < len(avKeys); i += batchGetCount {
		end := i + batchGetCount
		if end > len(avKeys) {
			end = len(avKeys)
		}
		output, err := ra.db.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{
			RequestItems: map[string]types.KeysAndAttributes{
				tableName: {Keys: avKeys[i:end]},
			},
		})
		if err != nil {
			return totalSize, fmt.Errorf("failed to get items: %s", err)
		}
		for _, item := range output.Responses[tableName] {
			totalSize += ra.guardrail.GetEstimatedSize(item)
		}
	}
	return totalSize, nil
}

func (ra *RiskAnalyzer) getRiskLevel(reason []string) engine.RiskLevel {
	level := engine.LowRiskLevel
	if len(reason) > 0 {
		level = engine.MediumRiskLevel
	}
	if len(reason) >= 2 {
		level = engine.HighRiskLevel
	}
	return level
}

func (ra *RiskAnalyzer) escalatedRiskLevel(reason []string) engine.RiskLevel {
	level := ra.getRiskLevel(reason)

	for _, item := range reason {
		if strings.Contains(item, "readonly") || strings.Contains(item, "protectedTable") {
			level = engine.HighRiskLevel
			break
		}
	}
	return level
}

func (ra *RiskAnalyzer) String(r engine.RiskLevel) string {
	switch r {
	case engine.LowRiskLevel:
		return "LOW"
	case engine.MediumRiskLevel:
		return "MEDIUM"
	case engine.HighRiskLevel:
		return "HIGH"
	case engine.CriticalRiskLevel:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
