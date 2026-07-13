package server

import (
	"bytes"
	"context"
	"dynamodb-sage/internal/llm"
	"dynamodb-sage/internal/metrics"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type JobResult struct {
	ID        string              `json:"id"`
	Result    *mcp.CallToolResult `json:"result,omitempty"`
	Done      chan struct{}       `json:"done,omitempty"`
	Error     error               `json:"error,omitempty"`
	StartedAt time.Time           `json:"startedAt,omitempty"`
}

func (srv *Server) addTools() {
	defs := srv.buildToolDefs()
	defsByName := make(map[string]llm.ToolDef, len(defs))
	for _, d := range defs {
		defsByName[d.Name] = d
	}

	// Register each tool directly — mcp.AddTool is generic and requires
	// explicit calls per handler type (Go cannot infer type params through
	// an any-typed variable, and ToolHandlerFor is a named type so a type
	// switch on an any-typed value never matches).
	register := func(name string, handler any, risk bool) {
		td := defsByName[name]
		tool := &mcp.Tool{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		}
		switch h := handler.(type) {
		case func(context.Context, *mcp.CallToolRequest, *ListTablesArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[ListTablesArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input ListTablesArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *DescribeTableArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[DescribeTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input DescribeTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *ScanTableArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[ScanTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input ScanTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			if risk {
				mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
			} else {
				mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, wrapped))
			}
		case func(context.Context, *mcp.CallToolRequest, *PutItemArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[PutItemArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input PutItemArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *QueryTableArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[QueryTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input QueryTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *GetItemArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[GetItemArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input GetItemArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *UpdateItemArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[UpdateItemArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input UpdateItemArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *DeleteItemArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[DeleteItemArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input DeleteItemArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *BatchGetItemArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[BatchGetItemArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input BatchGetItemArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *BatchPutItemsArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[BatchPutItemsArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input BatchPutItemsArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *BatchDeleteItemsArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[BatchDeleteItemsArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input BatchDeleteItemsArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *CreateOptimizedTableArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[CreateOptimizedTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input CreateOptimizedTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *DeleteTableArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[DeleteTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input DeleteTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *UpdateTableArgs) (*mcp.CallToolResult, any, error):
			wrapped := mcp.ToolHandlerFor[UpdateTableArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input UpdateTableArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, withRiskAnalysis(srv, wrapped)))
		case func(context.Context, *mcp.CallToolRequest, *UpdateTableTTLArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[UpdateTableTTLArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input UpdateTableTTLArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *ReadAuditLogsArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[ReadAuditLogsArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input ReadAuditLogsArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		case func(context.Context, *mcp.CallToolRequest, *GetJobResultArgs) (*mcp.CallToolResult, any, error):
			mcp.AddTool(srv.s, tool, instrumentMCP(srv, name, mcp.ToolHandlerFor[GetJobResultArgs, any](func(ctx context.Context, req *mcp.CallToolRequest, input GetJobResultArgs) (*mcp.CallToolResult, any, error) {
				return h(ctx, req, &input)
			})))
		default:
			log.Printf("WARNING: addTools: no matching type for tool %q (type %T)", name, handler)
		}
	}

	register("list_tables", srv.listTables, false)
	register("describe_table", srv.describeTable, false)
	register("scan_table", srv.scanTable, true)
	register("put_item", srv.putItem, true)
	register("query_table", srv.queryTable, false)
	register("get_item", srv.getItem, false)
	register("update_item", srv.updateItem, true)
	register("delete_item", srv.deleteItem, true)
	register("batch_get_items", srv.batchGetItems, true)
	register("batch_put_items", srv.batchPutItems, true)
	register("batch_delete_items", srv.batchDeleteItems, true)
	register("create_optimized_table", srv.createOptimizedTable, true)
	register("delete_table", srv.deleteTable, true)
	register("update_table", srv.updateTable, true)
	register("update_table_ttl", srv.updateTableTTL, false)
	register("read_audit_logs", srv.readAuditLogs, false)
	register("get_job_result", srv.getJobResult, false)

	srv.toolList = make([]ToolInfo, len(defs))
	for i, d := range defs {
		srv.toolList[i] = ToolInfo{Name: d.Name, Description: d.Description}
	}
}

func (srv *Server) buildToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{Name: "list_tables", Description: "List all DynamoDB tables", InputSchema: map[string]any{"type": "object"}},
		{Name: "describe_table", Description: "Get details about a DynamoDB table schema, indexes, and status", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{"tableName": map[string]any{"type": "string", "description": "The name of the table to describe. Example users, orders"}},
			"required":   []string{"tableName"},
		}},
		{Name: "scan_table", Description: "EXPENSIVE: Scans the entire table consuming high RCU. Only use as a last resort. ALWAYS prefer query_table with a GSI index if the access pattern is known.", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":                map[string]any{"type": "string", "description": "The name of the table to scan. Example users, orders"},
				"filterExpression":         map[string]any{"type": "string", "description": "The filter expression for the scan. Example #attr = :val"},
				"projectionExpression":     map[string]any{"type": "string", "description": "The projection expression for the scan"},
				"expressionAttributeValues": map[string]any{"type": "object", "description": "The expression attribute values for the scan"},
				"expressionAttributeNames":  map[string]any{"type": "object", "description": "The expression attribute names for the scan"},
				"indexName":                map[string]any{"type": "string", "description": "Optional: The name of an LSI or GSI to scan against. Omit to scan the base table."},
				"limit":                    map[string]any{"type": "integer", "description": "The maximum number of items to return", "default": defaultLimit},
				"exclusiveStartKey":        map[string]any{"type": "object", "description": "The exclusive start key for the scan"},
				"consistentRead":           map[string]any{"type": "boolean", "description": "If true, a strongly consistent read is used. Default is false (eventually consistent). Consistent reads consume more capacity units.", "default": false},
				"confirmation":             map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the scan operation", "default": false},
			},
			"required": []string{"tableName"},
		}},
		{Name: "query_table", Description: `PREFERRED over scan_table. Efficiently query a table using a key condition or GSI index. Much cheaper and faster than scanning. Use this whenever you know the partition key or have a GSI available.
		Common mistakes:
		1. The keyConditionExpression must include the table's partition key (HASH). If the table has a sort key (RANGE), you may optionally include it too. Omitting the partition key or using a wrong attribute name will cause: "Query condition missed key schema element".
		2. Attribute names in expressionAttributeNames must map to actual attribute names in the table, not arbitrary aliases. For example, if the hash key attribute is named "id", use {"#id": "id"} — NOT {"#id": "uid"}.
		3. The values in expressionAttributeValues must match the type of the key attribute. If "id" is a Number (N), pass a number like :id 1 — not a string like :id "matt".

		Example — querying a table with a composite key (hash + range):
		Table "Human" has key schema: id (HASH, Number), name (RANGE, String)

		Correct query:
		expressionAttributeNames: {"#id": "id", "#n": "name"}
		expressionAttributeValues: {":id": 1, ":n": "matt"}
		keyConditionExpression: "#id = :id AND #n = :n"

		If you only need the partition key:
		expressionAttributeNames: {"#id": "id"}
		expressionAttributeValues: {":id": 1}
		keyConditionExpression: "#id = :id"`, InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":                map[string]any{"type": "string", "description": "The name of the table to query. Example users, orders"},
				"indexName":                map[string]any{"type": "string", "description": "Optional: The name of an LSI or GSI to query against. Omit to query the base table."},
				"keyConditionExpression":   map[string]any{"type": "string", "description": "The condition expression for the query. Must include the partition key. Optionally include the sort key with AND. Example: \"#pk = :pkVal AND #sk = :skVal\""},
				"expressionAttributeValues": map[string]any{"type": "object", "description": "The expression attribute values for the query. Values must match the key attribute types (Number vs String). Example: {\":pkVal\": 1, \":skVal\": \"matt\"}"},
				"expressionAttributeNames":  map[string]any{"type": "object", "description": "The expression attribute names for the query. Maps placeholders to real attribute names. Example: {\"#pk\": \"id\", \"#sk\": \"name\"}"},
				"limit":                    map[string]any{"type": "integer", "description": "The maximum number of items to return", "default": defaultLimit},
				"exclusiveStartKey":        map[string]any{"type": "object", "description": "The exclusive start key for the query(pagination parameter)"},
				"consistentRead":           map[string]any{"type": "boolean", "description": "If true, a strongly consistent read is used. Default is false (eventually consistent). Consistent reads consume more capacity units.", "default": false},
			},
			"required": []string{"tableName", "keyConditionExpression", "expressionAttributeValues"},
		}},
		{Name: "put_item", Description: "Put an item into a DynamoDB table", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to put an item into. Example users, orders"},
				"item":         map[string]any{"type": "object", "description": "The item to put into the table as a plain JSON object. Example {\"id\": 1, \"name\": \"value\"}"},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the put item operation", "default": false},
			},
			"required": []string{"tableName", "item"},
		}},
		{Name: "get_item", Description: "Get an item from the table using primary key", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{"type": "string", "description": "The name of the table to get an item from. Example users, orders"},
				"key":       map[string]any{"type": "object", "description": "The primary key of the item to get as a plain JSON object. Example {\"id\": 1}"},
			},
			"required": []string{"tableName", "key"},
		}},
		{Name: "update_item", Description: "Update an item in the table using primary key", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":                map[string]any{"type": "string", "description": "The name of the table to update an item from. Example users, orders"},
				"key":                      map[string]any{"type": "object", "description": "The primary key of the item to update as a plain JSON object. Example {\"id\": 1, \"name\": \"Reggie\"}"},
				"updateExpression":         map[string]any{"type": "string", "description": "The update expression. Example SET #attr = :val"},
				"expressionAttributeNames":  map[string]any{"type": "object", "description": "the expression attribute names for the update. Example {\":attr\": \"attribute_name\"}"},
				"expressionAttributeValues": map[string]any{"type": "object", "description": "the expression attribute values for the update. Example {\":val\":{\"S\":\"active\"}}"},
				"conditionExpression":      map[string]any{"type": "string", "description": "An optional condition to evaluate before updating. Example attribute_exists(#attr)"},
				"returnValues":             map[string]any{"type": "string", "description": "The return values for the update. Choose from NONE, ALL_OLD, ALL_NEW, UPDATED_OLD, UPDATED_NEW"},
				"confirmation":             map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the update operation", "default": false},
			},
			"required": []string{"tableName", "key", "updateExpression"},
		}},
		{Name: "delete_item", Description: "Delete an item from a DynamoDB table", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to delete an item from. Example users, orders"},
				"key":          map[string]any{"type": "object", "description": "The primary key of the item to delete as a plain JSON object. Example {\"id\": 1}"},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the delete operation", "default": false},
			},
			"required": []string{"tableName", "key"},
		}},
		{Name: "batch_get_items", Description: "Batch get item from the table using primary key", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to batch get items from. Example users, orders"},
				"keys":         map[string]any{"type": "array", "description": "The primary keys of the items to batch get as plain JSON objects. Example [{\"id\": 1}, {\"id\": 2}]", "items": map[string]any{"type": "object"}},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the batch get operation", "default": false},
			},
			"required": []string{"tableName", "keys"},
		}},
		{Name: "batch_put_items", Description: "Put multiple items into a DynamoDB table", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table where new items go. Example users, orders"},
				"items":        map[string]any{"type": "array", "description": "The items to put into the table as plain JSON objects. Example [{\"id\": 1, \"name\": \"val1\"}, {\"id\": 2, \"name\": \"val2\"}]", "items": map[string]any{"type": "object"}},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the batch put operation", "default": false},
			},
			"required": []string{"tableName", "items"},
		}},
		{Name: "batch_delete_items", Description: "Delete multiple items in a DynamoDB table", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to delete the items from. Example users, orders"},
				"keys":         map[string]any{"type": "array", "description": "The primary keys of items to be deleted as plain JSON objects. Example [{\"id\": 1}, {\"id\": 2}]", "items": map[string]any{"type": "object"}},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the batch delete operation", "default": false},
			},
			"required": []string{"tableName", "keys"},
		}},
		{Name: "create_optimized_table", Description: "Create an optimized DynamoDB table using the best practices", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{"type": "string", "description": "The name of the table to create. Example users, orders"},
				"keySchema": map[string]any{
					"type":        "array",
					"description": "The key schema for the table",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"attributeName": map[string]any{"type": "string", "description": "The name of the attribute"},
							"keyType":       map[string]any{"type": "string", "description": "The type of the key", "enum": []string{string(types.KeyTypeHash), string(types.KeyTypeRange)}},
						},
						"required": []string{"attributeName", "keyType"},
					},
				},
				"attributeDefinitions": map[string]any{
					"type":        "array",
					"description": "The attributes for the table",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"attributeName": map[string]any{"type": "string", "description": "The name of the attribute"},
							"attributeType": map[string]any{"type": "string", "description": "The type of the attribute", "enum": []string{string(types.ScalarAttributeTypeS), string(types.ScalarAttributeTypeN), string(types.ScalarAttributeTypeB)}},
						},
						"required": []string{"attributeName", "attributeType"},
					},
				},
				"billingMode":        map[string]any{"type": "string", "description": "The billing mode for the table", "enum": []string{string(types.BillingModePayPerRequest), string(types.BillingModeProvisioned)}, "default": string(types.BillingModePayPerRequest)},
				"readCapacityUnits":  map[string]any{"type": "integer", "description": "The read capacity units for the table", "minimum": 1},
				"writeCapacityUnits": map[string]any{"type": "integer", "description": "The write capacity units for the table", "minimum": 1},
				"gsis":               map[string]any{"type": "array", "description": "The global secondary indexes for the table"},
				"lsis":               map[string]any{"type": "array", "description": "The local secondary indexes for the table"},
				"tags":               map[string]any{"type": "array", "description": "The tags for the table"},
				"confirmation":       map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the create operation", "default": false},
			},
			"required": []string{"tableName", "keySchema"},
		}},
		{Name: "delete_table", Description: "Delete a table (irreversible - ensure backups exist)", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to delete. Example users, orders"},
				"confirmation": map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the delete operation", "default": false},
			},
			"required": []string{"tableName"},
		}},
		{Name: "update_table", Description: "Update a table's provisioned throughput, billing mode, or global secondary indexes", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":                   map[string]any{"type": "string", "description": "The name of the table to update. Example users, orders"},
				"billingMode":                 map[string]any{"type": "string", "description": "The billing mode for the table", "enum": []string{string(types.BillingModePayPerRequest), string(types.BillingModeProvisioned)}},
				"provisionedThroughput":       map[string]any{"type": "object", "description": "The provisioned throughput for the table"},
				"attributeDefinitions":        map[string]any{"type": "array", "description": "The attributes needed for new global secondary indexes"},
				"globalSecondaryIndexUpdates": map[string]any{"type": "array", "description": "List of global secondary index updates. Each object must have exactly one of 'Create', 'Update', or 'Delete'."},
				"tags":                        map[string]any{"type": "array", "description": "The tags for the table"},
				"confirmation":                map[string]any{"type": "boolean", "description": "Please consider the warning carefully and then set to true to confirm the update operation", "default": false},
			},
			"required": []string{"tableName"},
		}},
		{Name: "update_table_ttl", Description: "Update a table's time to live", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName":    map[string]any{"type": "string", "description": "The name of the table to update"},
				"attributeName": map[string]any{"type": "string", "description": "The name of the attribute to set as TTL"},
				"enabled":      map[string]any{"type": "boolean", "description": "Enable or disable TTL"},
			},
			"required": []string{"tableName", "enabled", "attributeName"},
		}},
		{Name: "read_audit_logs", Description: "Read the audit log showing all DynamoDB operations that have been run recently including timestamp, user, table name, and capacity consumed.", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"startTime": map[string]any{"type": "string", "description": "The start time for the audit logs in RFC3339 format. Default is epoch."},
				"endTime":   map[string]any{"type": "string", "description": "The end time for the audit logs in RFC3339 format. Default is now."},
				"limit":     map[string]any{"type": "integer", "description": "The limit of audit logs to read", "default": defaultLimit},
			},
			"required": []string{"limit"},
		}},
		{Name: "get_job_result", Description: "Get the result of a job", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"jobId": map[string]any{"type": "string", "description": "The ID of the job"},
			},
			"required": []string{"jobId"},
		}},
	}
}

func withRiskAnalysis[In, Out any](srv *Server, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		m, err := srv.ConvertToMap(input)
		if err != nil {
			var empty Out
			return srv.formatWarningResult(fmt.Sprintf("failed to convert input to map: %s", err), req.Params.Name, m), empty, nil
		}

		reason, err := srv.checkRisk(ctx, req, m)
		if err != nil {
			var empty Out
			return srv.formatWarningResult(fmt.Sprintf("risk analysis failed: %s", err), req.Params.Name, m), empty, nil
		}

		if reason != "" {
			var empty Out
			return srv.formatWarningResult(fmt.Sprintf("detected risk: %s", reason), req.Params.Name, m), empty, nil
		}

		suggestions := srv.riskAnalyzer.TrackAndAdvise(ctx, req)
		msg := ""
		if len(suggestions) > 0 {
			msg = fmt.Sprintf("💡 **Suggestions** 💡\n\nPlease consider the following and show it to user :\n%s", strings.Join(suggestions, "\n"))
		}
		if srv.isLargeOperation(req) {
			id := uuid.New().String()
			start := time.Now()
			jobResult := &JobResult{ID: id, Done: make(chan struct{}), StartedAt: start}
			srv.jobStorage.Store(id, jobResult)
			metrics.JobStoragePending.Inc()

			jobPayload := struct {
				Input     In     `json:"input"`
				Operation string `json:"operation"`
			}{
				Input:     input,
				Operation: req.Params.Name,
			}
			inputPayload, err := json.Marshal(jobPayload)
			if err != nil {
				var empty Out
				return srv.errorResult(fmt.Sprintf("failed to marshal input: %v", err)), empty, nil
			}

			if srv.kafkaClient != nil {
				log.Printf("kafka producer exists, enqueueing task: %s", id)
				if err := srv.kafkaClient.Send(srv.heavyOpsTopic, id, inputPayload); err != nil {
					log.Printf("failed to send task to Kafka: %v", err)
					var empty Out
					return srv.errorResult(fmt.Sprintf("failed to enqueue task to Kafka: %v", err)), empty, nil
				}
			} else {
				log.Printf("kafka producer is nil, using go's native queue: %s", id)
				srv.queue.Submit(func(ctx context.Context) error {
					return srv.processHeavyOpForQueue(id, inputPayload)
				})
			}

			if msg != "" {
				msg = fmt.Sprintf("%s\n\nOperation %s queued for background processing. To see results call 'get_job_result' with job ID: %s", msg, req.Params.Name, id)
			} else {
				msg = fmt.Sprintf("Operation %s queued for background processing. To see results call 'get_job_result' with job ID: %s", req.Params.Name, id)
			}
			var empty Out
			return srv.successResult(msg), empty, nil
		}
		result, out, err := handler(ctx, req, input)
		if msg != "" {
			result.Content = append(result.Content, &mcp.TextContent{Text: msg})
		}
		return result, out, err
	}
}

func instrumentMCP[In, Out any](srv *Server, name string, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		metrics.MCPToolInvocationsTotal.WithLabelValues(name, srv.transport).Inc()
		start := time.Now()
		result, out, err := handler(ctx, req, input)
		dur := time.Since(start).Seconds()
		if srv.isLargeOperation(req) {
			metrics.MCPToolDurationSeconds.WithLabelValues(name, "heavy").Observe(dur)
		} else {
			metrics.MCPToolDurationSeconds.WithLabelValues(name, "light").Observe(dur)
		}
		if err != nil || (result != nil && result.IsError) {
			et := "execution"
			if err != nil {
				et = "go_error"
			}
			metrics.MCPToolErrorsTotal.WithLabelValues(name, et).Inc()
		}
		return result, out, err
	}
}

func (srv *Server) checkRisk(ctx context.Context, req *mcp.CallToolRequest, m map[string]any) (string, error) {
	if confirmed, _ := m["confirmation"].(bool); confirmed {
		metrics.RiskAnalysisConfirmedTotal.WithLabelValues(req.Params.Name).Inc()
		return "", nil
	}

	assessment, err := srv.riskAnalyzer.Analyze(ctx, req)
	if err != nil {
		return "", err
	}

	if assessment.IsRisk() {
		metrics.RiskAnalysisBlockedTotal.WithLabelValues(req.Params.Name, assessment.TableName, srv.riskAnalyzer.String(assessment.Level)).Inc()
		return assessment.Reason, nil
	}

	return "", nil
}

func (srv *Server) formatWarningResult(reason string, operation string, input map[string]interface{}) *mcp.CallToolResult {
	tableName := ""
	if tName, ok := input["tableName"].(string); ok {
		tableName = tName
	}
	msg := fmt.Sprintf("⚠️ **WARNING** ⚠️\n\n"+
		"**Operation:** %s\n"+
		"**Table:** %s\n"+
		"**Reason:** %s\n\n"+
		"**INSTRUCTIONS FOR AI:** when I return the warning, you need to ask user to confirm if they want to continue, call %s again with:\n"+
		"```json\n"+
		"{\n"+
		"  \"tableName\": \"%s\",\n"+
		"  \"confirmation\": true\n"+
		"}\n"+
		"```",
		operation, tableName, reason, operation, tableName)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: msg,
			},
		},
		IsError: false,
	}
}

func (srv *Server) ConvertToMap(input any) (map[string]any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	decoder := json.NewDecoder(bytes.NewBuffer(data))
	decoder.UseNumber()
	if err := decoder.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func (srv *Server) isLargeOperation(req *mcp.CallToolRequest) bool {
	switch req.Params.Name {
	case "create_optimized_table", "batch_put_items", "batch_delete_items":
		return true
	default:
		return false
	}
}
