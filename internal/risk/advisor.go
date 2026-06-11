package risk

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetItemArgs map[string]interface{}

type ScanTracker struct {
	Count      int
	LastSeen   time.Time
	Expression string
}

type TTLHandlerFunc func(input GetItemArgs) bool

func (ra *RiskAnalyzer) TrackAndAdvise(ctx context.Context, req *mcp.CallToolRequest) []string {
	var suggestions []string
	suggestions = append(suggestions, ra.AnalyzeKey(req)...)

	switch req.Params.Name {
	case "scan_table":
		suggestions = append(suggestions, ra.AnalyzeScan(ctx, req)...)
	case "put_item":
		suggestions = append(suggestions, ra.AnalyzeTTL(ctx, req, ra.hasTTLItem)...)
	case "batch_put_items":
		suggestions = append(suggestions, ra.AnalyzeTTL(ctx, req, ra.hasTTLInItemsArray)...)
	default:
	}

	return suggestions
}

func (ra *RiskAnalyzer) AnalyzeKey(req *mcp.CallToolRequest) []string {
	var suggestions []string
	// Extract the key condition from the request parameters
	var args GetItemArgs
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return suggestions
	}

	if args["key"] != nil {
		if keys, ok := args["key"].(map[string]interface{}); ok {
			for keyName := range keys {
				if hotPartitionKeys[strings.ToLower(keyName)] {
					suggestions = append(suggestions, fmt.Sprintf(
						"⚠️ SCHEMA WARNING: '%s' is being flagged as a partition key. Using low-cardinality fields or status flags as Partition Keys creates high risk for 'Hot Partitions', leading to throttling under scale.",
						keyName))
				}
			}
		}
	}

	return suggestions
}

func (ra *RiskAnalyzer) AnalyzeScan(ctx context.Context, req *mcp.CallToolRequest) []string {
	var suggestions []string
	// Extract the key condition from the request parameters
	var args GetItemArgs
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return suggestions
	}

	tableName, hasTableName := args["tableName"].(string)
	filterExp, hasFilterExp := args["filterExpression"].(string)
	if hasFilterExp && filterExp != "" && hasTableName && tableName != "" {
		if message := ra.validatMultipleScan(tableName, filterExp); message != "" {
			suggestions = append(suggestions, message)
		}
		if message := ra.validateAntiPatternScan(ctx, tableName, filterExp); message != "" {
			suggestions = append(suggestions, message)
		}
	}

	projectionExp, hasProjectionExp := args["projectionExpression"].(string)
	if (!hasProjectionExp || projectionExp == "") && hasTableName && tableName != "" {
		suggestions = append(suggestions, "⚠️ INFO: You are running a scan without a projection expression. Consider adding a projection expression to reduce the amount of data read from the table.")
	}

	indexName, hasIndexName := args["indexName"].(string)
	if hasIndexName && indexName != "" && hasTableName && tableName != "" {
		if message := ra.validateScanOnGsis(ctx, tableName, indexName); message != "" {
			suggestions = append(suggestions, message)
		}
	}

	return suggestions
}

func (ra *RiskAnalyzer) validatMultipleScan(tableName, filterExp string) string {
	hasInput := fmt.Sprintf("%s-%s", tableName, filterExp)
	hasher := md5.New()
	hasher.Write([]byte(hasInput))
	fingerprint := hex.EncodeToString(hasher.Sum(nil))

	tracker, _ := ra.scanHistory.LoadOrStore(fingerprint, &ScanTracker{
		Count:      0,
		LastSeen:   time.Now(),
		Expression: filterExp,
	})
	t := tracker.(*ScanTracker)
	t.Count++
	t.LastSeen = time.Now()
	if t.Count > 3 {
		return fmt.Sprintf(
			"⚠️ SCAN WARNING: '%s' is being scanned frequently. This may be a sign of an inefficient query pattern. Consider using a Secondary Index to optimize this query.",
			filterExp)
	}
	return ""
}

func (ra *RiskAnalyzer) validateAntiPatternScan(ctx context.Context, tableName, filterExp string) string {
	desc, err := ra.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return ""
	}
	for _, ksElement := range desc.Table.KeySchema {
		if ksElement.KeyType == types.KeyTypeHash {
			if strings.Contains(filterExp, string(*ksElement.AttributeName)) {
				return fmt.Sprintf(
					"⚠️ CRITICAL ANTI-PATTERN: You are running a Scan with a filter containing the table's primary Partition Key ('%s'). Change this tool call from scan_table to query_table and use KeyConditionExpression to decrease read capacity consumption by up to 99%%.",
					*ksElement.AttributeName)
			}
		}
	}
	return ""
}

func (ra *RiskAnalyzer) validateScanOnGsis(ctx context.Context, tableName string, indexName string) string {
	desc, err := ra.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return ""
	}
	for _, gsi := range desc.Table.GlobalSecondaryIndexes {
		if *gsi.IndexName == indexName && gsi.Projection.ProjectionType == types.ProjectionTypeAll {
			return fmt.Sprintf(
				"⚠️ INFO: Scan against GSI '%s' with ALL projection type. Consider using a Projection Expression to reduce the amount of data read from the table.",
				indexName)
		}
	}
	return ""
}

func (ra *RiskAnalyzer) AnalyzeTTL(ctx context.Context, req *mcp.CallToolRequest, handler TTLHandlerFunc) []string {
	var suggestions []string
	var args GetItemArgs
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return suggestions
	}

	tableName, hasTableName := args["tableName"].(string)
	if !hasTableName || tableName == "" {
		return suggestions
	}

	if hasTTL := handler(args); hasTTL {
		desc, err := ra.db.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			return suggestions
		}
		if desc.TimeToLiveDescription != nil {
			if desc.TimeToLiveDescription.TimeToLiveStatus == types.TimeToLiveStatusDisabled {
				suggestions = append(suggestions, fmt.Sprintf(
					"⚠️ INFO: Table '%s' has TTL attribute in the item but TTL is disabled. Consider enabling TTL to automatically delete old items and reduce storage costs.",
					tableName))
			}
		} else {
			suggestions = append(suggestions, fmt.Sprintf(
				"⚠️ INFO: Table '%s' has TTL enabled but you did not include TTL attribute in the item. Consider adding TTL attribute to the item to automatically delete old items and reduce storage costs.",
				tableName))
		}
	}
	return suggestions
}

func (ra *RiskAnalyzer) hasTTLItem(args GetItemArgs) bool {
	if item, ok := args["item"].(map[string]interface{}); ok {
		return ra.hasTTL(item)
	}
	return false
}

func (ra *RiskAnalyzer) hasTTLInItemsArray(args GetItemArgs) bool {
	if items, ok := args["items"].([]interface{}); ok {
		for _, item := range items {
			if itemMap, ok := item.(map[string]interface{}); ok && ra.hasTTL(itemMap) {
				return true
			}
		}
	}
	return false
}

func (ra *RiskAnalyzer) hasTTL(item interface{}) bool {
	switch v := item.(type) {
	case map[string]interface{}:
		for _, key := range []string{"ttl", "expiresAt", "expire_at", "expires_on", "expiry", "timeout", "expiry_date"} {
			if _, ok := v[key]; ok {
				return true
			}
		}
	}
	return false
}
