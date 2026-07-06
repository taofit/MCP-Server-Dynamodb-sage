package server

import (
	"bytes"
	"context"
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
	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "list_tables",
		Description: "List all DynamoDB tables",
		InputSchema: map[string]any{
			"type": "object",
		},
	}, instrumentMCP(srv, "list_tables", srv.listTables))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "describe_table",
		Description: "Get details about a DynamoDB table schema, indexes, and status",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to describe. Example users, orders",
				},
			},
			"required": []string{"tableName"},
		},
	}, instrumentMCP(srv, "describe_table", srv.describeTable))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "scan_table",
		Description: "⚠️ EXPENSIVE: Scans the entire table consuming high RCU. Only use as a last resort when no key or GSI is available. ALWAYS prefer query_table with a GSI index if the access pattern is known. Scanning large tables can be very costly.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to scan. Example users, orders",
				},
				"filterExpression": map[string]any{
					"type":        "string",
					"description": "The filter expression for the scan. Example #attr = :val",
				},
				"projectionExpression": map[string]any{
					"type":        "string",
					"description": "The projection expression for the scan",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "The expression attribute values for the scan",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "The expression attribute names for the scan",
				},
				"indexName": map[string]any{
					"type":        "string",
					"description": "Optional: The name of an LSI or GSI to scan against. Omit to scan the base table.",
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
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the scan operation",
					"default":     false,
				},
			},
			"required": []string{"tableName"},
		},
	}, instrumentMCP(srv, "scan_table", withRiskAnalysis(srv, srv.scanTable)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "put_item",
		Description: "Put an item into a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to put an item into. Example users, orders",
				},
				"item": map[string]any{
					"type":        "object",
					"description": "The item to put into the table as a plain JSON object. Example {\"id\": 1, \"name\": \"value\"}",
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the put item operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "item"},
		},
	}, instrumentMCP(srv, "put_item", withRiskAnalysis(srv, srv.putItem)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name: "query_table",
		Description: `PREFERRED over scan_table. Efficiently query a table using a key condition or GSI index. Much cheaper and faster than scanning. Use this whenever you know the partition key or have a GSI available.
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
		keyConditionExpression: "#id = :id"`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to query. Example users, orders",
				},
				"indexName": map[string]any{
					"type":        "string",
					"description": "Optional: The name of an LSI or GSI to query against. Omit to query the base table.",
				},
				"keyConditionExpression": map[string]any{
					"type":        "string",
					"description": "The condition expression for the query. Must include the partition key. Optionally include the sort key with AND. Example: \"#pk = :pkVal AND #sk = :skVal\"",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "The expression attribute values for the query. Values must match the key attribute types (Number vs String). Example: {\":pkVal\": 1, \":skVal\": \"matt\"}",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "The expression attribute names for the query. Maps placeholders to real attribute names. Example: {\"#pk\": \"id\", \"#sk\": \"name\"}",
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
	}, instrumentMCP(srv, "query_table", srv.queryTable))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_put_items",
		Description: "Put multiple items into a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table where new items go. Example users, orders",
				},
				"items": map[string]any{
					"type":        "array",
					"description": "The items to put into the table as plain JSON objects. Example [{\"id\": 1, \"name\": \"val1\"}, {\"id\": 2, \"name\": \"val2\"}]",
					"items": map[string]any{
						"type": "object",
					},
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the bat	ch put operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "items"},
		},
	}, instrumentMCP(srv, "batch_put_items", withRiskAnalysis(srv, srv.batchPutItems)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_delete_items",
		Description: "Delete multiple items in a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete the items from. Example users, orders",
				},
				"keys": map[string]any{
					"type":        "array",
					"description": "The primary keys of items to be deleted as plain JSON objects. Example [{\"id\": 1}, {\"id\": 2}]",
					"items": map[string]any{
						"type": "object",
					},
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the batch delete operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "keys"},
		},
	}, instrumentMCP(srv, "batch_delete_items", withRiskAnalysis(srv, srv.batchDeleteItems)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "delete_item",
		Description: "Delete an item from a DynamoDB table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete an item from. Example users, orders",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primary key of the item to delete as a plain JSON object. Example {\"id\": 1}",
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the delete operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "key"},
		},
	}, instrumentMCP(srv, "delete_item", withRiskAnalysis(srv, srv.deleteItem)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "get_item",
		Description: "Get an item from the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to get an item from. Example users, orders",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primary key of the item to get as a plain JSON object. Example {\"id\": 1}",
				},
			},
			"required": []string{"tableName", "key"},
		},
	}, instrumentMCP(srv, "get_item", srv.getItem))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "update_item",
		Description: "Update an item in the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to update an item from. Example users, orders",
				},
				"key": map[string]any{
					"type":        "object",
					"description": "The primary key of the item to update as a plain JSON object. Example {\"id\": 1, \"name\": \"Reggie\"}",
				},
				"updateExpression": map[string]any{
					"type":        "string",
					"description": "The update expression. Example SET #attr = :val",
				},
				"expressionAttributeNames": map[string]any{
					"type":        "object",
					"description": "the expression attribute names for the update. Example {\":attr\": \"attribute_name\"}",
				},
				"expressionAttributeValues": map[string]any{
					"type":        "object",
					"description": "the expression attribute values for the update. Example {\":val\":{\"S\":\"active\"}}",
				},
				"conditionExpression": map[string]any{
					"type":        "string",
					"description": "An optional condition to evaluate before updating. Example attribute_exists(#attr)",
				},
				"returnValues": map[string]any{
					"type":        "string",
					"description": "The return values for the update. Choose from NONE, ALL_OLD, ALL_NEW, UPDATED_OLD, UPDATED_NEW",
					"enum": []string{
						"NONE",
						"ALL_OLD",
						"ALL_NEW",
						"UPDATED_OLD",
						"UPDATED_NEW",
					},
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the update operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "key", "updateExpression"},
		},
	}, instrumentMCP(srv, "update_item", withRiskAnalysis(srv, srv.updateItem)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "batch_get_items",
		Description: "Batch get item from the table using primary key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to batch get items from. Example users, orders",
				},
				"keys": map[string]any{
					"type":        "array",
					"description": "The primary keys of the items to batch get as plain JSON objects. Example [{\"id\": 1}, {\"id\": 2}]",
					"items": map[string]any{
						"type": "object",
					},
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the batch get operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "keys"},
		},
	}, instrumentMCP(srv, "batch_get_items", withRiskAnalysis(srv, srv.batchGetItems)))

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
	}, instrumentMCP(srv, "read_audit_logs", srv.readAuditLogs))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "create_optimized_table",
		Description: "Create an optimized DynamoDB table using the best practices",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to create. Example users, orders",
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
					"description": "The read capacity units for the table",
					"minimum":     1,
				},
				"writeCapacityUnits": map[string]any{
					"type":        "integer",
					"description": "The write capacity units for the table",
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
							"projectionType": map[string]any{
								"type":        "string",
								"description": "The projection type for the index",
								"enum":        []string{string(types.ProjectionTypeAll), string(types.ProjectionTypeKeysOnly), string(types.ProjectionTypeInclude)},
								"default":     string(types.ProjectionTypeAll),
							},
							"nonKeyAttributes": map[string]any{
								"type":        "array",
								"description": "The non-key attributes to project. Required when ProjectionType is INCLUDE, and the list of attributes must include all attributes that you need to query.",
								"items": map[string]any{
									"type": "string",
								},
							},
						},
						"required": []string{"indexName", "partitionKey", "projectionType"},
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
				"tags": map[string]any{
					"type":        "array",
					"description": "The tags for the table example: [{\"key\":\"environment\",\"value\":[\"staging\"]},{\"key\":\"name\",\"value\":[\"dynamodb-table\"]}]",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"key": map[string]any{
								"type":        "string",
								"description": "The key of the tag",
							},
							"value": map[string]any{
								"type":        "array",
								"description": "The value of the tag",
								"items": map[string]any{
									"type": "string",
								},
							},
						},
					},
					"default": []any{
						map[string]any{
							"key": "environment",
							"value": []any{
								"staging",
							},
						},
					},
					"required": []string{"key", "value"},
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the create operation",
					"default":     false,
				},
			},
			"required": []string{"tableName", "keySchema"},
		},
	}, instrumentMCP(srv, "create_optimized_table", withRiskAnalysis(srv, srv.createOptimizedTable)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "delete_table",
		Description: "Delete a table",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to delete. Example users, orders",
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the delete operation",
					"default":     false,
				},
			},
			"required": []string{"tableName"},
		},
	}, instrumentMCP(srv, "delete_table", withRiskAnalysis(srv, srv.deleteTable)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "update_table",
		Description: "Update a table's provisioned throughput, billing mode, or global secondary indexes",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to update. Example users, orders",
				},
				"confirmation": map[string]any{
					"type":        "boolean",
					"description": "Please consider the warning carefully and then set to true to confirm the update operation",
					"default":     false,
				},
				"provisionedThroughput": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"readCapacityUnits": map[string]any{
							"type":        "integer",
							"description": "The read capacity units for the table. Use 0 to let the server normalize it to 1.",
							"minimum":     0,
						},
						"writeCapacityUnits": map[string]any{
							"type":        "integer",
							"description": "The write capacity units for the table. Use 0 to let the server normalize it to 1.",
							"minimum":     0,
						},
					},
					"required": []string{"readCapacityUnits", "writeCapacityUnits"},
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
				"tags": map[string]any{
					"type":        "array",
					"description": "The tags for the table example: [{\"key\":\"Environment\",\"value\":[\"Production\"]},{\"key\":\"Name\",\"value\":[\"DynamoDBTable\"]}]",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"key": map[string]any{
								"type":        "string", 
								"description": "The key of the tag",
							},
							"value": map[string]any{
								"type":        "array",
								"description": "The value of the tag",
								"items": map[string]any{
									"type": "string",
								},
							},
						},
						"required": []string{"key", "value"},
					},
				},
			},
			"required": []string{"tableName"},
		},
	}, instrumentMCP(srv, "update_table", withRiskAnalysis(srv, srv.updateTable)))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "update_table_ttl",
		Description: "Update a table's time to live",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to update",
				},
				"attributeName": map[string]any{
					"type":        "string",
					"description": "The name of the attribute to set as TTL",
				},
				"enabled": map[string]any{
					"type":        "boolean",
					"description": "Enable or disable TTL",
				},
			},
			"required": []string{"tableName", "enabled", "attributeName"},
		},
	}, instrumentMCP(srv, "update_table_ttl", srv.updateTableTTL))

	mcp.AddTool(srv.s, &mcp.Tool{
		Name:        "get_job_result",
		Description: "Get the result of a job",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"jobId": map[string]any{
					"type":        "string",
					"description": "The ID of the job",
				},
			},
			"required": []string{"jobId"},
		},
	}, instrumentMCP(srv, "get_job_result", srv.getJobResult))

	srv.toolList = []ToolInfo{
		{Name: "list_tables", Description: "List all DynamoDB tables"},
		{Name: "describe_table", Description: "Get details about a DynamoDB table schema, indexes, and status"},
		{Name: "scan_table", Description: "Expensive: scan entire table (use query_table when possible)"},
		{Name: "put_item", Description: "Put an item into a DynamoDB table"},
		{Name: "query_table", Description: "Query a table using key condition (preferred over scan)"},
		{Name: "batch_put_items", Description: "Put multiple items into a DynamoDB table"},
		{Name: "batch_delete_items", Description: "Delete multiple items in a DynamoDB table"},
		{Name: "delete_item", Description: "Delete an item from a DynamoDB table"},
		{Name: "get_item", Description: "Get an item from the table using primary key"},
		{Name: "update_item", Description: "Update an item in the table using primary key"},
		{Name: "batch_get_items", Description: "Batch get items from the table using primary keys"},
		{Name: "read_audit_logs", Description: "Read the audit log showing recent DynamoDB operations"},
		{Name: "create_optimized_table", Description: "Create an optimized DynamoDB table using best practices"},
		{Name: "delete_table", Description: "Delete a table"},
		{Name: "update_table", Description: "Update table throughput, billing mode, or GSIs"},
		{Name: "update_table_ttl", Description: "Update a table's time to live"},
		{Name: "get_job_result", Description: "Get the result of a job"},
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
