package server

import (
	"context"
	"dynamodb-sage/internal/engine"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	db        *dynamodb.Client
	s         *mcp.Server
	guardrail *engine.Guardrail
}

type ListTablesArgs struct {
}

type DescribeTableArgs struct {
	TableName string `json:"tableName"`
}

type ScanTableArgs struct {
	TableName                 string         `json:"tableName"`
	ExpressionAttributeValues map[string]any `json:"expressionAttributeValues"`
	FilterExpression          string         `json:"filterExpression"`
	ProjectionExpression      string         `json:"projectionExpression"`
	Limit                     int32          `json:"limit"`
	ExclusiveStartKey         map[string]any `json:"exclusiveStartKey"`
}

type PutItemArgs struct {
	TableName string         `json:"tableName"`
	Item      map[string]any `json:"item"`
}

type QueryTableArgs struct {
	TableName                 string         `json:"tableName"`
	KeyConditionExpression    string         `json:"keyConditionExpression"`
	ExpressionAttributeValues map[string]any `json:"expressionAttributeValues"`
	Limit                     int32          `json:"limit"`
	ExclusiveStartKey         map[string]any `json:"exclusiveStartKey"`
}

type BatchPutItemsArgs struct {
	TableName string           `json:"tableName"`
	Items     []map[string]any `json:"items"`
}

type DeleteItemArgs struct {
	TableName string         `json:"tableName"`
	Key       map[string]any `json:"key"`
}

type GetItemArgs struct {
	TableName string         `json:"tableName"`
	Key       map[string]any `json:"key"`
}

const batchSize = 25

func New(db *dynamodb.Client) *Server {

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "dynamo-sage",
		Version: "1.0.0",
	}, nil)

	guardrail := engine.NewGuardrail(100, 20)
	srv := &Server{
		db:        db,
		s:         s,
		guardrail: guardrail,
	}
	srv.addTools()

	return srv
}

func (srv *Server) SSEHandler() http.Handler {
	sseHandler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set up a single route with CORS support for the inspector
		// Allow the MCP Inspector (or any origin) to connect
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		sseHandler.ServeHTTP(w, r)
	})
}

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
		Description: "Read items from a DynamoDB table (returns up to 20 items)",
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
		Description: "Query a table using a key condition expression and optional filter expression (returns up to 20 items each time)",
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
}

func (srv *Server) queryTable(ctx context.Context, req *mcp.CallToolRequest, args *QueryTableArgs) (*mcp.CallToolResult, any, error) {
	var startKey map[string]types.AttributeValue
	if args.ExclusiveStartKey != nil {
		var err error
		startKey, err = attributevalue.MarshalMap(args.ExclusiveStartKey)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when marshaling exclusive start key: %v", err),
					},
				},
				IsError: true,
			}, nil, nil
		}
	}

	attributevalues, err := attributevalue.MarshalMap(args.ExpressionAttributeValues)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when marshaling expression attribute values: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	if args.KeyConditionExpression == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: "KeyConditionExpression is required",
				},
			},
			IsError: true,
		}, nil, nil
	}
	limit, warning := srv.guardrail.EnforceLimit(args.Limit)

	result, err := srv.db.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &args.TableName,
		KeyConditionExpression:    &args.KeyConditionExpression,
		ExpressionAttributeValues: attributevalues,
		Limit:                     &limit,
		ExclusiveStartKey:         startKey,
	})

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when querying table: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	items := []map[string]any{}
	err = attributevalue.UnmarshalListOfMaps(result.Items, &items)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when unmarshaling items: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	itemsText := fmt.Sprintf("DynamoDB Table: \"%s\"\nQueried %d items from table %s:", args.TableName, len(items), args.TableName)
	scrubbedItems := srv.guardrail.ScrubItems(items)
	for i, item := range scrubbedItems {
		itemJson, _ := json.Marshal(item)
		itemsText += fmt.Sprintf("\n[%d] %s", i+1, string(itemJson))
	}

	if len(result.LastEvaluatedKey) > 0 {
		nextKey := map[string]any{}
		err = attributevalue.UnmarshalMap(result.LastEvaluatedKey, &nextKey)
		jsonKey, _ := json.Marshal(nextKey)
		if err == nil {
			itemsText += fmt.Sprintf("\n\nNote: There are more items available. Use the 'exclusiveStartKey' option with value: %s to fetch the next page of items.\n", string(jsonKey))
		} else {
			itemsText += fmt.Sprintf("\n\nNote: There are more items available, but failed to unmarshal the next key: %v\n", err)
		}
	}

	if warning != "" {
		itemsText += "\nNote: " + warning
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: itemsText,
			},
		},
	}, nil, nil
}

func (srv *Server) putItem(ctx context.Context, req *mcp.CallToolRequest, args *PutItemArgs) (*mcp.CallToolResult, any, error) {
	// Convert the plain Go map into a map of DynamoDB AttributeValues
	av, err := attributevalue.MarshalMap(args.Item)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error marshaling item: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	_, err = srv.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &args.TableName,
		Item:      av,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when putting item: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully put item into table %s", args.TableName),
			},
		},
	}, nil, nil
}

func (srv *Server) listTables(ctx context.Context, req *mcp.CallToolRequest, args *ListTablesArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when listing tables: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	tables := strings.Join(out.TableNames, ", ")
	if tables == "" {
		tables = "(no tables found)"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("DynamoDB Tables: %s", tables),
			},
		},
	}, nil, nil
}
func (srv *Server) describeTable(ctx context.Context, req *mcp.CallToolRequest, args *DescribeTableArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &args.TableName,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when describing table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	var tableName = "Unknown"
	if out.Table.TableName != nil {
		tableName = *out.Table.TableName
	}

	var itemCount int64 = 0
	if out.Table.ItemCount != nil {
		itemCount = *out.Table.ItemCount
	}

	var sizeBytes int64 = 0
	if out.Table.TableSizeBytes != nil {
		sizeBytes = *out.Table.TableSizeBytes
	}

	// Format the output in a readable way
	details := fmt.Sprintf("Table: %s\nStatus: %s\nItem Count: %d\nSize (Bytes): %d\n",
		tableName, out.Table.TableStatus, itemCount, sizeBytes)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: details,
			},
		},
	}, nil, nil
}

func (srv *Server) scanTable(ctx context.Context, req *mcp.CallToolRequest, args *ScanTableArgs) (*mcp.CallToolResult, any, error) {
	limit, warning := srv.guardrail.EnforceLimit(args.Limit)
	input := &dynamodb.ScanInput{
		TableName: &args.TableName,
		Limit:     &limit,
	}
	if args.ProjectionExpression != "" {
		input.ProjectionExpression = &args.ProjectionExpression
	}
	if args.FilterExpression != "" {
		input.FilterExpression = &args.FilterExpression
	}
	if args.ExpressionAttributeValues != nil {
		var err error
		input.ExpressionAttributeValues, err = attributevalue.MarshalMap(args.ExpressionAttributeValues)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when marshaling expression attribute values: %v", err),
					},
				},
				IsError: true,
			}, nil, nil
		}
	}
	if args.ExclusiveStartKey != nil {
		startKey, err := attributevalue.MarshalMap(args.ExclusiveStartKey)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when marshaling exclusive start key: %v", err),
					},
				},
				IsError: true,
			}, nil, nil
		}
		input.ExclusiveStartKey = startKey
	}
	out, err := srv.db.Scan(ctx, input)

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when scanning table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when scanning table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	// Unmarshal the DynamoDB items into a list of plain Go maps
	items := []map[string]any{}
	err = attributevalue.UnmarshalListOfMaps(out.Items, &items)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error unmarshaling items: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	// For a simple text representation of the items
	itemsText := fmt.Sprintf("DynamoDB Table: \"%s\"\nScanned %d items from table %s:", args.TableName, len(items), args.TableName)
	scrubbedItems := srv.guardrail.ScrubItems(items)
	for i, item := range scrubbedItems {
		itemJson, _ := json.Marshal(item)
		itemsText += fmt.Sprintf("\n[%d] %s", i+1, string(itemJson))
	}
	// Check if there are more items available
	if len(out.LastEvaluatedKey) > 0 {
		nextKey := map[string]any{}
		err = attributevalue.UnmarshalMap(out.LastEvaluatedKey, &nextKey)
		jsonKey, _ := json.Marshal(nextKey)
		if err == nil {
			itemsText += fmt.Sprintf("\n\nNote: There are more items available. Use the 'exclusiveStartKey' option with value: %s to fetch the next page of items.", string(jsonKey))
		} else {
			itemsText += fmt.Sprintf("\n\nNote: There are more items available, but failed to unmarshal the next key: %v", err)
		}
	}
	if warning != "" {
		itemsText += "\nNote: " + warning
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: itemsText,
			},
		},
	}, nil, nil
}

func (srv *Server) batchPutItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchPutItemsArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("No items to put into table %s", args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}

	items := []types.WriteRequest{}
	for _, item := range args.Items {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when marshalling item %v: %v", item, err),
					},
				},
				IsError: true,
			}, nil, nil
		}
		writeRequest := types.WriteRequest{
			PutRequest: &types.PutRequest{
				Item: av,
			},
		}
		items = append(items, writeRequest)
	}

	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				args.TableName: items[start:end],
			},
		}

		_, err := srv.db.BatchWriteItem(ctx, input)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when batch putting items to table %s : %v", args.TableName, err),
					},
				},
				IsError: true,
			}, nil, nil
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully put %d items into table %s", len(args.Items), args.TableName),
			},
		},
	}, nil, nil
}

func (srv *Server) deleteItem(ctx context.Context, req *mcp.CallToolRequest, args *DeleteItemArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateDelete(args.TableName); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Validation error: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	if len(args.Key) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Key is required for deleting an item from table %s", args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}
	av, err := attributevalue.MarshalMap(args.Key)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error: Failed to marshal key %v for table %s: %v", args.Key, args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	input := &dynamodb.DeleteItemInput{
		TableName:    &args.TableName,
		Key:          av,
		ReturnValues: types.ReturnValueAllOld,
	}

	output, err := srv.db.DeleteItem(ctx, input)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when deleting item %v from table %s: %v", args.Key, args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	if len(output.Attributes) == 0 {
		keyJson, _ := json.Marshal(args.Key)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Item with key %s not found in table %s", string(keyJson), args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}

	attributes := map[string]any{}
	attributevalue.UnmarshalMap(output.Attributes, &attributes)

	scrubbed := srv.guardrail.ScrubItems([]map[string]any{attributes})
	itemJson, _ := json.Marshal(scrubbed[0])
	keyJson, _ := json.Marshal(args.Key)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully deleted item %s from table: %s. Attributes: %s", string(keyJson), args.TableName, string(itemJson)),
			},
		},
	}, nil, nil
}

func (srv *Server) getItem(ctx context.Context, req *mcp.CallToolRequest, args *GetItemArgs) (*mcp.CallToolResult, any, error) {
	if args.Key == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when getting item for key %v from table %s: Key is required", args.Key, args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}

	av, err := attributevalue.MarshalMap(args.Key)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when marshalling key %v for table %s: %v", args.Key, args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	input := &dynamodb.GetItemInput{
		TableName: &args.TableName,
		Key:       av,
	}

	output, err := srv.db.GetItem(ctx, input)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when getting item from table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	item := map[string]any{}
	attributevalue.UnmarshalMap(output.Item, &item)
	keyJson, _ := json.Marshal(args.Key)

	if len(item) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Item with key %s not found in table %s", string(keyJson), args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}
	
	scrubbedItem := srv.guardrail.ScrubItems([]map[string]any{item})[0]
	itemJson, _ := json.Marshal(scrubbedItem)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Item with key %s from table %s: %s", string(keyJson), args.TableName, string(itemJson)),
			},
		},
	}, nil, nil
}
