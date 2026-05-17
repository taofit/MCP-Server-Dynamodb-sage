package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
	unprocessedItemMsg := ""
	totalUnprocessed := 0

	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batchItems := items[start:end]
		if err := srv.guardrail.ValidateBatchSize(batchItems); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when batch putting items to table %s: %v", args.TableName, err),
					},
				},
				IsError: true,
			}, nil, nil
		}
		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				args.TableName: batchItems,
			},
		}
		for i := 0; i < 3; i++ {
			output, err := srv.db.BatchWriteItem(ctx, input)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("Error when batch putting items to table %s: %v", args.TableName, err),
						},
					},
					IsError: true,
				}, nil, nil
			}
			if len(output.UnprocessedItems) > 0 {
				if i == 2 {
					for _, reqs := range output.UnprocessedItems {
						totalUnprocessed += len(reqs)
					}
				} else {
					input.RequestItems = output.UnprocessedItems
				}
			} else {
				break
			}
		}
	}

	if totalUnprocessed > 0 {
		unprocessedItemMsg = fmt.Sprintf("\nWarning: %d items were not processed due to provisioned throughput exceeded when batch putting items to table %s.", totalUnprocessed, args.TableName)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully put %d items into table %s%s", len(args.Items)-totalUnprocessed, args.TableName, unprocessedItemMsg),
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

func (srv *Server) updateItem(ctx context.Context, req *mcp.CallToolRequest, args *UpdateItemArgs) (*mcp.CallToolResult, any, error) {
	if args.Key == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when updating item for table: %s key is required", args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}
	key, err := attributevalue.MarshalMap(args.Key)
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

	input := &dynamodb.UpdateItemInput{
		TableName: &args.TableName,
		Key:       key,
	}

	if len(args.ExpressionAttributeNames) > 0 {
		input.ExpressionAttributeNames = args.ExpressionAttributeNames
	}
	if len(args.ExpressionAttributeValues) > 0 {
		attriValue, err := attributevalue.MarshalMap(args.ExpressionAttributeValues)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error when marshalling expression attribute value %v for table %s: %v", args.ExpressionAttributeValues, args.TableName, err),
					},
				},
				IsError: true,
			}, nil, nil
		}
		input.ExpressionAttributeValues = attriValue
	}
	if args.ConditionExpression != "" {
		input.ConditionExpression = &args.ConditionExpression
	}
	if args.UpdateExpression != "" {
		input.UpdateExpression = &args.UpdateExpression
	}
	if args.ReturnValue != "" {
		input.ReturnValues = types.ReturnValue(args.ReturnValue)
	}

	output, err := srv.db.UpdateItem(ctx, input)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when updating item %v from table %s: %v", args.Key, args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}
	var attributes map[string]any
	var scrubbedAttributes []map[string]any
	if len(output.Attributes) != 0 {
		attributevalue.UnmarshalMap(output.Attributes, &attributes)
		scrubbedAttributes = srv.guardrail.ScrubItems([]map[string]any{attributes})
	}
	var attributesMsg = ""
	if len(scrubbedAttributes) != 0 {
		scrubbedAttributeJson, _ := json.Marshal(scrubbedAttributes[0])
		attributesMsg += fmt.Sprintf(", Attributes: %s", scrubbedAttributeJson)
	}
	keyJson, _ := json.Marshal(args.Key)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully updated item %v from table %s%s", string(keyJson), args.TableName, attributesMsg),
			},
		},
	}, nil, nil
}

func (srv *Server) batchGetItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchGetItemArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Keys) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error: No keys provided for batch get from table %s", args.TableName),
				},
			},
			IsError: true,
		}, nil, nil
	}

	keys := make([]map[string]types.AttributeValue, 0, len(args.Keys))
	existingKeys := make(map[string]bool)

	for _, key := range args.Keys {
		keyJson, _ := json.Marshal(key)
		keyStr := string(keyJson)
		if existingKeys[keyStr] {
			continue
		}
		existingKeys[keyStr] = true

		av, err := attributevalue.MarshalMap(key)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error failed to marshal key %v for table %s: %v", key, args.TableName, err),
					},
				},
				IsError: true,
			}, nil, nil
		}
		keys = append(keys, av)
	}

	outputResponse := []map[string]types.AttributeValue{}
	unprocessedKeys := make(map[string]types.KeysAndAttributes)
	unprocessedMsg := ""

	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}

		requestItems := map[string]types.KeysAndAttributes{}
		chunkKey := keys[start:end]
		requestItems[args.TableName] = types.KeysAndAttributes{
			Keys: chunkKey,
		}
		input := &dynamodb.BatchGetItemInput{
			RequestItems: requestItems,
		}

		for i := 0; i < 3; i++ {
			output, err := srv.db.BatchGetItem(ctx, input)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("Error when batch getting items from table %s: %v", args.TableName, err),
						},
					},
					IsError: true,
				}, nil, nil
			}
			outputResponse = append(outputResponse, output.Responses[args.TableName]...)
			if len(output.UnprocessedKeys) > 0 {
				input.RequestItems = output.UnprocessedKeys
				// If this is the last retry attempt, save these as permanently unprocessed
				if i == 2 {
					// Accumulate failed keys across all 25-item chunks
					for tableName, ka := range output.UnprocessedKeys {
						tmpKeys := unprocessedKeys[tableName]
						tmpKeys.Keys = append(tmpKeys.Keys, ka.Keys...)
						unprocessedKeys[tableName] = tmpKeys
					}
				}
			} else {
				break
			}
		}
	}

	if len(unprocessedKeys) > 0 {
		var failedList []map[string]any
		for _, ka := range unprocessedKeys {
			for _, keyAV := range ka.Keys {
				keyAVTrans := map[string]any{}
				attributevalue.UnmarshalMap(keyAV, &keyAVTrans)
				failedList = append(failedList, keyAVTrans)
			}
		}
		failedJson, _ := json.Marshal(failedList)
		unprocessedMsg = fmt.Sprintf(", Warning: %d keys were not processed (provisioned throughput exceeded). Failed keys: %s", len(failedList), string(failedJson))
	}
	scrubbedItems := []map[string]any{}
	for _, item := range outputResponse {
		scrubbedItem := map[string]any{}
		attributevalue.UnmarshalMap(item, &scrubbedItem)
		scrubbedItems = append(scrubbedItems, scrubbedItem)
	}

	scrubbedItems = srv.guardrail.ScrubItems(scrubbedItems)

	itemStrings := []string{}
	for _, item := range scrubbedItems {
		itemMsg, _ := json.Marshal(item)
		itemStrings = append(itemStrings, string(itemMsg))
	}

	itemsJson, _ := json.Marshal(itemStrings)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Successfully batch get %d items from table %s: %s%s", len(scrubbedItems), args.TableName, string(itemsJson), unprocessedMsg),
			},
		},
	}, nil, nil
}
