package server

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (srv *Server) addTools() {
	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "list_tables",
		Description: "List all DynamoDB tables",
		InputSchema: map[string]any{
			"type": "object",
		},
	}, srv.listTables)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "describe_table",
		Description: "Get details about a DynamoDB table schema, indexes, and status",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to describe",
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.describeTable)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "scan_table",
		Description: "⚠️ EXPENSIVE: Scans the entire table consuming high RCU. Only use as a last resort when no key or GSI is available. ALWAYS prefer query_table with a GSI index if the access pattern is known. Scanning large tables can be very costly.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to scan",
				},
				"filterExpression": map[string]any{
					"type":        "string",
					"description": "The filter expression for the scan",
				},
				"projectionExpression": map[string]any{
					"type":        "string",
					"description": "The projection expression for the scan",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "The expression attribute values for the scan",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "The maximum number of items to return",
					"default":     defaultLimit,
				},
				"exclusiveStartKey": map[string]any{
					"type":        "object",
					"description": "The exclusive start key for the scan",
				},
				"consistentRead": map[string]any{
					"type":        "boolean",
					"description": "If true, a strongly consistent read is used. Default is false (eventually consistent). Consistent reads consume more capacity units.",
					"default":     false,
				},
			},
			"required": []string{"tableName"},
		},
	}, withRiskAnalysis(srv, srv.scanTable))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "put_item",
		Description: "Put an item into a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to put an item into",
				},
				"item": map[string]any{
					"type":        "object",
					"description": "The item to put into the table, in JSON format",
				},
				"conditionExpression": map[string]any{
					"type":        "string",
					"description": "A condition that must be satisfied in order for a conditional put to succeed. If the condition is not met, the item is not put.",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "One or more substitution tokens for attribute names in an expression.",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "One or more values that can be substituted in an expression.",
				},
				"returnValues": map[string]any{
					"type":        "string",
					"description": "Use RETURN_VALUES to get the item attributes as they appeared before they were updated, or immediately after they were updated. The default value is NONE. There are also NONE, ALL_OLD, UPDATED_OLD, ALL_NEW, UPDATED_NEW.",
					"enum": []string{
						"NONE",
						"ALL_OLD",
						"UPDATED_OLD",
						"ALL_NEW",
						"UPDATED_NEW",
					},
				},
			},
			"required": []string{"tableName", "item"},
		},
	}, srv.putItem)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "query_table",
		Description: "PREFERRED over scan_table. Efficiently query a table using a key condition or GSI index. Much cheaper and faster than scanning. Use this whenever you know the partition key or have a GSI available.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to query",
				},
				"indexName": map[string]any{
					"type":        "string",
					"description": "Optional: The name of an LSI or GSI to query against. Omit to query the base table.",
				},
				"keyConditionExpression": map[string]any{
					"type":        "string",
					"description": "The condition expression for the query",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "The expression attribute values for the query",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "The expression attribute names for the query",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "The maximum number of items to return",
					"default":     defaultLimit,
				},
				"exclusiveStartKey": map[string]any{
					"type":        "object",
					"description": "The exclusive start key for the query(pagination parameter)",
				},
				"consistentRead": map[string]any{
					"type":        "boolean",
					"description": "If true, a strongly consistent read is used. Default is false (eventually consistent). Consistent reads consume more capacity units.",
					"default":     false,
				},
			},
			"required": []string{"tableName", "keyConditionExpression", "expressionAttributeValues"},
		},
	}, srv.queryTable)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_put_items",
		Description: "Put multiple items into a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table where new items go",
				},
				"items": map[string]any{
					"type":        "array",
					"description": "The items put into the table in JSON format",
					"items": map[string]any{
						"type": "object",
					},
				},
				"returnValues": map[string]any{
					"type":        "string",
					"description": "Use RETURN_VALUES to get the item attributes as they appeared before they were updated, or immediately after they were updated. The default value is NONE. There are also NONE, ALL_OLD, UPDATED_OLD, ALL_NEW, UPDATED_NEW.",
					"enum": []string{
						"NONE",
						"ALL_OLD",
						"UPDATED_OLD",
						"ALL_NEW",
						"UPDATED_NEW",
					},
				},
			},
			"required": []string{"tableName", "items"},
		},
	}, srv.batchPutItems)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_delete_items",
		Description: "Delete multiple items in a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete the items from",
				},
				"keys": map[string]any{
					"type":        "array",
					"description": "The keys of items to be deleted from the table",
					"items": map[string]any{
						"type": "object",
					},
				},
			},
			"required": []string{"tableName", "keys"},
		},
	}, srv.batchDeleteItems)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "delete_item",
		Description: "Delete an item from a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete an item from",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primary key of the item to delete in JSON format",
				},
			},
			"required": []string{"tableName", "key"},
		},
	}, srv.deleteItem)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "get_item",
		Description: "Get an item from the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to get an item from",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primay key of the item to get in JSON format",
				},
			},
			"required": []string{"tableName", "key"},
		},
	}, srv.getItem)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "update_item",
		Description: "Update an item in the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to update an item from",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primary key of the item to update in JSON format",
				},
				"updateExpression": map[string]any{
					"type":        "string",
					"description": "the expression to update",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "the expression attribute names for the update",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "the expression attribute values for the update",
				},
				"conditionExpression": map[string]any{
					"type":        "string",
					"description": "A optional condition to evaluate before updating",
				},
				"returnValues": map[string]any{
					"type":        "string",
					"description": "the return values for the update",
					"enum": []string{
						"NONE",
						"ALL_OLD",
						"ALL_NEW",
						"UPDATED_OLD",
						"UPDATED_NEW",
					},
				},
			},
			"required": []string{"tableName", "key", "updateExpression"},
		},
	}, srv.updateItem)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_get_item",
		Description: "Batch get item from the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to batch get items from",
				},
				"keys": map[string]any{
					"type":        "array",
					"description": "The primary keys of the items to batch get in JSON format",
					"items": map[string]any{
						"type": "object",
					},
				},
			},
			"required": []string{"tableName", "keys"},
		},
	}, srv.batchGetItems)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "read_audit_logs",
		Description: "Read the audit log showing all DynamoDB operations that have been run recently (creates, puts, queries, deletes, etc.) including timestamp, user, table name, and capacity consumed.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"startTime": map[string]any{
					"type":        "string",
					"description": "The start time for the audit logs in RFC3339 format (e.g. \"2026-05-27T00:00:00Z\", \"2026-05-26T15:30:00Z\"). Default is epoch (1970-01-01T00:00:00Z).",
				},
				"endTime": map[string]any{
					"type":        "string",
					"description": "The end time for the audit logs in RFC3339 format (e.g. \"2026-05-27T12:00:00Z\", \"2026-05-26T18:00:00Z\"). Default is now (current time).",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "The limit of audit logs to read",
					"default":     defaultLimit,
				},
			},
			"required": []string{"limit"},
		},
	}, srv.readAuditLogs)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "create_optimized_table",
		Description: "Create an optimized DynamoDB table using the best practices",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to create",
				},
				"keySchema": map[string]any{
					"type":        "array",
					"description": "The key schema for the table",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"attributeName": map[string]any{
								"type":        "string",
								"description": "The name of the attribute",
							},
							"keyType": map[string]any{
								"type":        "string",
								"description": "The type of the key",
								"enum":        []string{string(types.KeyTypeHash), string(types.KeyTypeRange)},
							},
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
							"attributeName": map[string]any{
								"type":        "string",
								"description": "The name of the attribute",
							},
							"attributeType": map[string]any{
								"type":        "string",
								"description": "The type of the attribute",
								"enum":        []string{string(types.ScalarAttributeTypeS), string(types.ScalarAttributeTypeN), string(types.ScalarAttributeTypeB)},
							},
						},
						"required": []string{"attributeName", "attributeType"},
					},
				},
				"billingMode": map[string]any{
					"type":        "string",
					"description": "The billing mode for the table",
					"enum":        []string{string(types.BillingModePayPerRequest), string(types.BillingModeProvisioned)},
					"default":     string(types.BillingModePayPerRequest),
				},
				"readCapacityUnits": map[string]any{
					"type":        "integer",
					"description": "The read capacity units for the table when billing mode is PROVISIONED. Minimum value is 1.",
					"minimum":     1,
				},
				"writeCapacityUnits": map[string]any{
					"type":        "integer",
					"description": "The write capacity units for the table when billing mode is PROVISIONED. Minimum value is 1.",
					"minimum":     1,
				},
				"gsis": map[string]any{
					"type":        "array",
					"description": "The global secondary indexes for the table",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"indexName": map[string]any{
								"type":        "string",
								"description": "The name of the index",
							},
							"partitionKey": map[string]any{
								"type":        "string",
								"description": "The name of the partition key",
							},
							"sortKey": map[string]any{
								"type":        "string",
								"description": "The name of the sort key",
							},
							"readCapacityUnits": map[string]any{
								"type":        "integer",
								"description": "The read capacity units for the index",
								"minimum":     1,
							},
							"writeCapacityUnits": map[string]any{
								"type":        "integer",
								"description": "The write capacity units for the index",
								"minimum":     1,
							},
						},
						"required": []string{"indexName", "partitionKey"},
					},
				},
				"lsis": map[string]any{
					"type":        "array",
					"description": "The local secondary indexes for the table",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"indexName": map[string]any{
								"type":        "string",
								"description": "The name of the index",
							},
							"sortKey": map[string]any{
								"type":        "string",
								"description": "The name of the sort key",
							},
						},
						"required": []string{"indexName", "sortKey"},
					},
				},
			},
			"required": []string{"tableName", "keySchema"},
		},
	}, srv.createOptimizedTable)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "delete_table",
		Description: "Delete a table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete",
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.deleteTable)

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "update_table",
		Description: "Update a table's provisioned throughput, billing mode, or global secondary indexes",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to update",
				},
				"provisionedThroughput": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"readCapacity": map[string]any{
							"type":        "integer",
							"description": "The read capacity units for the table",
							"minimum":     1,
						},
						"writeCapacity": map[string]any{
							"type":        "integer",
							"description": "The write capacity units for the table",
							"minimum":     1,
						},
					},
					"required": []string{"readCapacity", "writeCapacity"},
				},
				"billingMode": map[string]any{
					"type":        "string",
					"description": "The billing mode for the table",
					"enum":        []string{string(types.BillingModePayPerRequest), string(types.BillingModeProvisioned)},
					"default":     string(types.BillingModePayPerRequest),
				},
				"attributeDefinitions": map[string]any{
					"type":        "array",
					"description": "The attributes needed for new global secondary indexes",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"attributeName": map[string]any{
								"type":        "string",
								"description": "The name of the attribute",
							},
							"attributeType": map[string]any{
								"type":        "string",
								"description": "The type of the attribute",
								"enum":        []string{string(types.ScalarAttributeTypeS), string(types.ScalarAttributeTypeN), string(types.ScalarAttributeTypeB)},
							},
						},
						"required": []string{"attributeName", "attributeType"},
					},
				},
				"globalSecondaryIndexUpdates": map[string]any{
					"type":        "array",
					"description": "List of global secondary index updates. Each object must have exactly one of 'Create', 'Update', or 'Delete'.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"Create": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"IndexName": map[string]any{"type": "string"},
									"KeySchema": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"AttributeName": map[string]any{"type": "string"},
												"KeyType":       map[string]any{"type": "string", "enum": []string{"HASH", "RANGE"}},
											},
											"required": []string{"AttributeName", "KeyType"},
										},
									},
									"Projection": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"ProjectionType": map[string]any{"type": "string", "enum": []string{"ALL", "KEYS_ONLY", "INCLUDE"}},
										},
										"required": []string{"ProjectionType"},
									},
									"ProvisionedThroughput": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"ReadCapacityUnits":  map[string]any{"type": "integer"},
											"WriteCapacityUnits": map[string]any{"type": "integer"},
										},
									},
								},
								"required": []string{"IndexName", "KeySchema", "Projection", "ProvisionedThroughput"},
							},
							"Update": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"IndexName": map[string]any{"type": "string"},
									"ProvisionedThroughput": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"ReadCapacityUnits":  map[string]any{"type": "integer"},
											"WriteCapacityUnits": map[string]any{"type": "integer"},
										},
										"required": []string{"ReadCapacityUnits", "WriteCapacityUnits"},
									},
								},
								"required": []string{"IndexName", "ProvisionedThroughput"},
							},
							"Delete": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"IndexName": map[string]any{"type": "string"},
								},
								"required": []string{"IndexName"},
							},
						},
					},
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.updateTable)
}

func withRiskAnalysis[In, Out any](srv *Server, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		assessment, err := srv.riskAnalyzer.Analyze(ctx, req)
		if err != nil {
			var empty Out
			return srv.formatWarningResult(fmt.Sprintf("risk analysis failed: %s", err), req.Params.Name), empty, nil
		}
		if assessment.IsHighRisk() {
			var empty Out
			return srv.formatWarningResult(fmt.Sprintf("high risk detected: %s", assessment.Reason), req.Params.Name), empty, nil
		}
		return handler(ctx, req, input)
	}
}

func (srv *Server) formatWarningResult(reason string, operation string) *mcp.CallToolResult {
	msg := fmt.Sprintf("⚠️ **WARNING** ⚠️\n\n"+
		"**Reason:** %s\n"+
		"**Calculated Cost/Impact:** High Resource Consumption.\n\n"+
		"To complete this action, please explicitly reply with: **'Confirm execution of %s'**.", reason, operation)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: msg,
			},
		},
		IsError: false,
	}
}
