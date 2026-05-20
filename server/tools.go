package server

import (
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
				},
				"exclusiveStartKey": map[string]any{
					"type":        "object",
					"description": "The exclusive start key for the scan",
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.scanTable)

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
				},
				"exclusiveStartKey": map[string]any{
					"type":        "object",
					"description": "The exclusive start key for the query(pagination parameter)",
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
		Description: "Read audit logs from sqlite database",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				// "startTime": map[string]any{
				// 	"type":        "string",
				// 	"description": "The start time for the audit logs",
				// },
				// "endTime": map[string]any{
				// 	"type":        "string",
				// 	"description": "The end time for the audit logs",
				// },
				"limit": map[string]any{
					"type":        "integer",
					"description": "The limit of audit logs to read",
					"default":     20,
				},
			},
			"required": []string{"limit"},
		},
	}, srv.readAuditLogs)
}
