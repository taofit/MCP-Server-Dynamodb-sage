package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
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
			return srv.errorResult(fmt.Sprintf("Error when marshaling exclusive start key: %v", err)), nil, nil
		}
	}

	attributevalues, err := attributevalue.MarshalMap(args.ExpressionAttributeValues)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error when marshaling expression attribute values: %v", err)), nil, nil
	}

	if args.KeyConditionExpression == "" {
		return srv.errorResult("KeyConditionExpression is required"), nil, nil
	}
	limit, warning := srv.guardrail.EnforceLimit(args.Limit)

	output, err := srv.db.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &args.TableName,
		KeyConditionExpression:    &args.KeyConditionExpression,
		ExpressionAttributeNames:  args.ExpressionAttributeNames,
		ExpressionAttributeValues: attributevalues,
		Limit:                     &limit,
		ExclusiveStartKey:         startKey,
		ReturnConsumedCapacity:    types.ReturnConsumedCapacityTotal,
	})

	if err != nil {
		var cc *types.ConsumedCapacity
		if output != nil {
			cc = output.ConsumedCapacity
		}
		srv.sendAuditLog("query_table", args.TableName, "RCU", cc, err)

		return srv.errorResult(fmt.Sprintf("Error when querying table: %v", err)), nil, nil
	}
	items := []map[string]any{}
	err = attributevalue.UnmarshalListOfMaps(output.Items, &items)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error when unmarshaling items: %v", err)), nil, nil
	}
	itemsText := fmt.Sprintf("DynamoDB Table: \"%s\"\nQueried %d items from table %s:", args.TableName, len(items), args.TableName)
	scrubbedItems := srv.guardrail.ScrubItems(items)
	for i, item := range scrubbedItems {
		itemJson, _ := json.Marshal(item)
		itemsText += fmt.Sprintf("\n[%d] %s", i+1, string(itemJson))
	}

	if len(output.LastEvaluatedKey) > 0 {
		nextKey := map[string]any{}
		err = attributevalue.UnmarshalMap(output.LastEvaluatedKey, &nextKey)
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

	srv.sendAuditLog("query_table", args.TableName, "RCU", output.ConsumedCapacity, nil)

	return srv.successResult(itemsText), nil, nil
}

func (srv *Server) putItem(ctx context.Context, req *mcp.CallToolRequest, args *PutItemArgs) (*mcp.CallToolResult, any, error) {
	// Convert the plain Go map into a map of DynamoDB AttributeValues
	av, err := attributevalue.MarshalMap(args.Item)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error marshaling item: %v", err)), nil, nil
	}
	if res := srv.validateSchema(args.TableName, av); res != nil {
		return res, nil, nil
	}

	output, err := srv.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:              &args.TableName,
		Item:                   av,
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	})
	if err != nil {
		var cc *types.ConsumedCapacity
		if output != nil {
			cc = output.ConsumedCapacity
		}
		srv.sendAuditLog("put_item", args.TableName, "WCU", cc, err)

		return srv.errorResult(fmt.Sprintf("Error when putting item: %v", err)), nil, nil
	}
	srv.sendAuditLog("put_item", args.TableName, "WCU", output.ConsumedCapacity, nil)

	return srv.successResult(fmt.Sprintf("Successfully put item into table %s", args.TableName)), nil, nil
}

func (srv *Server) listTables(ctx context.Context, req *mcp.CallToolRequest, args *ListTablesArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		srv.sendAuditLog("list_tables", "", "", nil, err)
		return srv.errorResult(fmt.Sprintf("Error when listing tables: %v", err)), nil, nil
	}

	tables := strings.Join(out.TableNames, ", ")
	if tables == "" {
		tables = "(no tables found)"
	}
	srv.sendAuditLog("list_tables", "", "", nil, nil)

	return srv.successResult(fmt.Sprintf("DynamoDB Tables: %s", tables)), nil, nil
}

func (srv *Server) describeTable(ctx context.Context, req *mcp.CallToolRequest, args *DescribeTableArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &args.TableName,
	})
	if err != nil {
		srv.sendAuditLog("describe_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("Error when describing table %s: %v", args.TableName, err)), nil, nil
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

	var attributeDefinitions []string
	for _, att := range out.Table.AttributeDefinitions {
		attributeDefinitions = append(attributeDefinitions, fmt.Sprintf("%s (%s)", *att.AttributeName, att.AttributeType))
	}

	var keySchema []string

	for _, key := range out.Table.KeySchema {
		keySchema = append(keySchema, fmt.Sprintf("%s (%s)", *key.AttributeName, key.KeyType))
	}

	var gsis []string
	if out.Table.GlobalSecondaryIndexes != nil {
		for _, gsi := range out.Table.GlobalSecondaryIndexes {
			var keySchema []string
			for _, key := range gsi.KeySchema {
				keySchema = append(keySchema, fmt.Sprintf("%s (%s)", *key.AttributeName, key.KeyType))
			}
			gsis = append(gsis, fmt.Sprintf("%s: %s\n", *gsi.IndexName, strings.Join(keySchema, ", ")))
		}
	}
	var lsis []string
	if out.Table.LocalSecondaryIndexes != nil {
		for _, lsi := range out.Table.LocalSecondaryIndexes {
			var keySchema []string
			for _, key := range lsi.KeySchema {
				keySchema = append(keySchema, fmt.Sprintf("%s (%s)", *key.AttributeName, key.KeyType))
			}
			lsis = append(lsis, fmt.Sprintf("%s: %s\n", *lsi.IndexName, strings.Join(keySchema, "")))
		}
	}

	// Format the output in a readable way
	details := fmt.Sprintf("Table: %s\nStatus: %s\nItem Count: %d\nSize (Bytes): %d\nKey Schema: %s\nAttribute Definitions: %s\n",
		tableName, out.Table.TableStatus, itemCount, sizeBytes, strings.Join(keySchema, ", "), strings.Join(attributeDefinitions, ", "))
	if len(gsis) > 0 {
		details += fmt.Sprintf("Global Secondary Indexes (GSIs): \n%s", strings.Join(gsis, ""))
	}
	if len(lsis) > 0 {
		details += fmt.Sprintf("Local Secondary Indexes (LSIs): \n%s", strings.Join(lsis, ""))
	}
	srv.sendAuditLog("describe_table", args.TableName, "", nil, nil)
	return srv.successResult(details), nil, nil
}

func (srv *Server) scanTable(ctx context.Context, req *mcp.CallToolRequest, args *ScanTableArgs) (*mcp.CallToolResult, any, error) {
	limit, warning := srv.guardrail.EnforceLimit(args.Limit)
	input := &dynamodb.ScanInput{
		TableName:              &args.TableName,
		Limit:                  &limit,
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
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
			return srv.errorResult(fmt.Sprintf("Error when marshaling expression attribute values: %v", err)), nil, nil
		}
	}
	if args.ExclusiveStartKey != nil {
		startKey, err := attributevalue.MarshalMap(args.ExclusiveStartKey)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshaling exclusive start key: %v", err)), nil, nil
		}
		input.ExclusiveStartKey = startKey
	}
	out, err := srv.db.Scan(ctx, input)

	if err != nil {
		var cc *types.ConsumedCapacity
		if out != nil {
			cc = out.ConsumedCapacity
		}
		srv.sendAuditLog("scan_table", args.TableName, "RCU", cc, err)
		return srv.errorResult(fmt.Sprintf("Error when scanning table %s: %v", args.TableName, err)), nil, nil
	}

	// Unmarshal the DynamoDB items into a list of plain Go maps
	items := []map[string]any{}
	err = attributevalue.UnmarshalListOfMaps(out.Items, &items)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error unmarshaling items: %v", err)), nil, nil
	}

	// For a simple text representation of the items
	itemsText := fmt.Sprintf("⚠️ Warning: Scan reads the entire table and is costly in RCU. Consider using query_table with a GSI for better performance and lower cost.\n\nDynamoDB Table: \"%s\"\nScanned %d items from table %s:", args.TableName, len(items), args.TableName)
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

	srv.sendAuditLog("scan_table", args.TableName, "RCU", out.ConsumedCapacity, nil)

	return srv.successResult(itemsText), nil, nil
}

func (srv *Server) batchPutItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchPutItemsArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return srv.errorResult(fmt.Sprintf("No items to put into table %s", args.TableName)), nil, nil
	}

	items := []types.WriteRequest{}
	for _, item := range args.Items {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshalling item %v: %v", item, err)), nil, nil
		}
		if res := srv.validateSchema(args.TableName, av); res != nil {
			return res, nil, nil
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

	ccList := []types.ConsumedCapacity{}
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batchItems := items[start:end]
		if err := srv.guardrail.ValidateBatchSize(batchItems); err != nil {
			return srv.errorResult(fmt.Sprintf("Error when batch putting items to table %s: %v", args.TableName, err)), nil, nil
		}
		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				args.TableName: batchItems,
			},
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		}
		for i := 0; i < 3; i++ {
			output, err := srv.db.BatchWriteItem(ctx, input)
			if output != nil && output.ConsumedCapacity != nil {
				ccList = append(ccList, output.ConsumedCapacity...)
			}
			if err != nil {
				srv.sendAuditLog("batch_put_items", args.TableName, "WCU", srv.aggregateConsumedCapacity(ccList), err)
				return srv.errorResult(fmt.Sprintf("Error when batch putting items to table %s: %v", args.TableName, err)), nil, nil
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
	srv.sendAuditLog("batch_put_items", args.TableName, "WCU", srv.aggregateConsumedCapacity(ccList), nil)

	if totalUnprocessed > 0 {
		unprocessedItemMsg = fmt.Sprintf("\nWarning: %d items were not processed due to provisioned throughput exceeded when batch putting items to table %s.", totalUnprocessed, args.TableName)
	}

	return srv.successResult(fmt.Sprintf("Successfully put %d items into table %s%s", len(args.Items)-totalUnprocessed, args.TableName, unprocessedItemMsg)), nil, nil
}

func (srv *Server) batchDeleteItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchDeleteItemsArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if len(args.Keys) == 0 {
		return srv.errorResult(fmt.Sprintf("No keys provided to delete from table %s", args.TableName)), nil, nil
	}
	items := []types.WriteRequest{}
	for _, key := range args.Keys {
		av, err := attributevalue.MarshalMap(key)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshaling key %v from table %s: %v", key, args.TableName, err)), nil, nil
		}

		items = append(items, types.WriteRequest{
			DeleteRequest: &types.DeleteRequest{
				Key: av,
			},
		})
	}

	unprocessedItemMsg := ""
	totalUnprocessed := 0
	ccList := []types.ConsumedCapacity{}
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}

		batchItems := items[start:end]

		if err := srv.guardrail.ValidateBatchSize(batchItems); err != nil {
			return srv.errorResult(fmt.Sprintf("Error when batch deleting items from table %s: %v", args.TableName, err)), nil, nil
		}

		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				args.TableName: batchItems,
			},
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		}
		for i := 0; i < 3; i++ {
			output, err := srv.db.BatchWriteItem(ctx, input)
			if output != nil && output.ConsumedCapacity != nil {
				ccList = append(ccList, output.ConsumedCapacity...)
			}
			if err != nil {
				srv.sendAuditLog("batch_delete_items", args.TableName, "WCU", srv.aggregateConsumedCapacity(ccList), err)
				return srv.errorResult(fmt.Sprintf("Error when batch deleting items from table %s: %v", args.TableName, err)), nil, nil
			}
			if len(output.UnprocessedItems) > 0 {
				if i == 2 {
					for _, req := range output.UnprocessedItems {
						totalUnprocessed += len(req)
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
		unprocessedItemMsg = fmt.Sprintf("\nWarning: %d items were not deleted due to provisioned throughput constraints from table %s.", totalUnprocessed, args.TableName)
	}

	srv.sendAuditLog("batch_delete_items", args.TableName, "WCU", srv.aggregateConsumedCapacity(ccList), nil)

	return srv.successResult(fmt.Sprintf("Successfully deleted %d items from table %s%s", len(args.Keys)-totalUnprocessed, args.TableName, unprocessedItemMsg)), nil, nil
}

func (srv *Server) deleteItem(ctx context.Context, req *mcp.CallToolRequest, args *DeleteItemArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if len(args.Key) == 0 {
		return srv.errorResult(fmt.Sprintf("Key is required for deleting an item from table %s", args.TableName)), nil, nil
	}
	av, err := attributevalue.MarshalMap(args.Key)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error: Failed to marshal key %v for table %s: %v", args.Key, args.TableName, err)), nil, nil
	}
	input := &dynamodb.DeleteItemInput{
		TableName:              &args.TableName,
		Key:                    av,
		ReturnValues:           types.ReturnValueAllOld,
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	}

	output, err := srv.db.DeleteItem(ctx, input)
	if err != nil {
		var consumedCapacity *types.ConsumedCapacity
		if output != nil {
			consumedCapacity = output.ConsumedCapacity
		}
		srv.sendAuditLog("delete_item", args.TableName, "WCU", consumedCapacity, err)
		return srv.errorResult(fmt.Sprintf("Error when deleting item %v from table %s: %v", args.Key, args.TableName, err)), nil, nil
	}

	srv.sendAuditLog("delete_item", args.TableName, "WCU", output.ConsumedCapacity, nil)

	if len(output.Attributes) == 0 {
		keyJson, _ := json.Marshal(args.Key)
		return srv.errorResult(fmt.Sprintf("Item with key %s not found in table %s", string(keyJson), args.TableName)), nil, nil
	}

	attributes := map[string]any{}
	attributevalue.UnmarshalMap(output.Attributes, &attributes)

	scrubbed := srv.guardrail.ScrubItems([]map[string]any{attributes})
	itemJson, _ := json.Marshal(scrubbed[0])
	keyJson, _ := json.Marshal(args.Key)

	return srv.successResult(fmt.Sprintf("Successfully deleted item %s from table: %s. Attributes: %s", string(keyJson), args.TableName, string(itemJson))), nil, nil
}

func (srv *Server) getItem(ctx context.Context, req *mcp.CallToolRequest, args *GetItemArgs) (*mcp.CallToolResult, any, error) {
	if args.Key == nil {
		return srv.errorResult(fmt.Sprintf("Error when getting item for key %v from table %s: Key is required", args.Key, args.TableName)), nil, nil
	}
	av, err := attributevalue.MarshalMap(args.Key)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error when marshalling key %v for table %s: %v", args.Key, args.TableName, err)), nil, nil
	}

	input := &dynamodb.GetItemInput{
		TableName:              &args.TableName,
		Key:                    av,
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	}

	output, err := srv.db.GetItem(ctx, input)
	if err != nil {
		var consumedCapacity *types.ConsumedCapacity
		if output != nil {
			consumedCapacity = output.ConsumedCapacity
		}
		srv.sendAuditLog("get_item", args.TableName, "RCU", consumedCapacity, err)
		return srv.errorResult(fmt.Sprintf("Error when getting item from table %s: %v", args.TableName, err)), nil, nil
	}
	item := map[string]any{}
	attributevalue.UnmarshalMap(output.Item, &item)
	keyJson, _ := json.Marshal(args.Key)

	if len(item) == 0 {
		return srv.errorResult(fmt.Sprintf("Item with key %s not found in table %s", string(keyJson), args.TableName)), nil, nil
	}

	scrubbedItem := srv.guardrail.ScrubItems([]map[string]any{item})[0]
	itemJson, _ := json.Marshal(scrubbedItem)

	srv.sendAuditLog("get_item", args.TableName, "RCU", output.ConsumedCapacity, nil)

	return srv.successResult(fmt.Sprintf("Successfully got item with key %s from table %s: %s", string(keyJson), args.TableName, string(itemJson))), nil, nil
}

func (srv *Server) updateItem(ctx context.Context, req *mcp.CallToolRequest, args *UpdateItemArgs) (*mcp.CallToolResult, any, error) {
	if args.Key == nil {
		return srv.errorResult(fmt.Sprintf("Error when updating item for table: %s key is required", args.TableName)), nil, nil
	}
	key, err := attributevalue.MarshalMap(args.Key)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error when marshalling key %v for table %s: %v", args.Key, args.TableName, err)), nil, nil
	}

	input := &dynamodb.UpdateItemInput{
		TableName:              &args.TableName,
		Key:                    key,
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	}

	if len(args.ExpressionAttributeNames) > 0 {
		input.ExpressionAttributeNames = args.ExpressionAttributeNames
	}
	if len(args.ExpressionAttributeValues) > 0 {
		attriValue, err := attributevalue.MarshalMap(args.ExpressionAttributeValues)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshalling expression attribute value %v for table %s: %v", args.ExpressionAttributeValues, args.TableName, err)), nil, nil
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
		var cc *types.ConsumedCapacity
		if output != nil {
			cc = output.ConsumedCapacity
		}
		srv.sendAuditLog("update_item", args.TableName, "WCU", cc, err)
		return srv.errorResult(fmt.Sprintf("Error when updating item %v from table %s: %v", args.Key, args.TableName, err)), nil, nil
	}
	srv.sendAuditLog("update_item", args.TableName, "WCU", output.ConsumedCapacity, nil)

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

	return srv.successResult(fmt.Sprintf("Successfully updated item %v from table %s%s", string(keyJson), args.TableName, attributesMsg)), nil, nil
}

func (srv *Server) batchGetItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchGetItemArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Keys) == 0 {
		return srv.errorResult(fmt.Sprintf("Error: No keys provided for batch get from table %s", args.TableName)), nil, nil
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
			return srv.errorResult(fmt.Sprintf("Error failed to marshal key %v for table %s: %v", key, args.TableName, err)), nil, nil
		}
		keys = append(keys, av)
	}

	outputResponse := []map[string]types.AttributeValue{}
	unprocessedKeys := make(map[string]types.KeysAndAttributes)
	unprocessedMsg := ""
	ccList := []types.ConsumedCapacity{}

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
			RequestItems:           requestItems,
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		}

		for i := 0; i < 3; i++ {
			output, err := srv.db.BatchGetItem(ctx, input)
			if output != nil && output.ConsumedCapacity != nil {
				ccList = append(ccList, output.ConsumedCapacity...)
			}
			if err != nil {
				srv.sendAuditLog("batch_get_items", args.TableName, "RCU", srv.aggregateConsumedCapacity(ccList), err)
				return srv.errorResult(fmt.Sprintf("Error when batch getting items from table %s: %v", args.TableName, err)), nil, nil
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

	srv.sendAuditLog("batch_get_items", args.TableName, "RCU", srv.aggregateConsumedCapacity(ccList), nil)

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
		itemTrans, _ := json.Marshal(item)
		itemStrings = append(itemStrings, string(itemTrans))
	}

	itemsJson, _ := json.Marshal(itemStrings)
	return srv.successResult(fmt.Sprintf("Successfully batch get %d items from table %s: %s%s", len(scrubbedItems), args.TableName, string(itemsJson), unprocessedMsg)), nil, nil
}

func (srv *Server) createOptimizedTable(ctx context.Context, req *mcp.CallToolRequest, args *CreateTableArgs) (*mcp.CallToolResult, any, error) {
	keySchema, hashKey := srv.getKeySchema(args.KeySchema)

	providedAttributeTypes := make(map[string]types.ScalarAttributeType)
	for _, ad := range args.AttributeDefinitions {
		if _, ok := providedAttributeTypes[ad.AttributeName]; !ok {
			providedAttributeTypes[ad.AttributeName] = types.ScalarAttributeType(ad.AttributeType)
		}
	}

	attributeDefinitionMap := make(map[string]types.AttributeDefinition)

	addAttribute := func(attributeName string) {
		if _, ok := attributeDefinitionMap[attributeName]; ok {
			return
		}
		attributeType, ok := providedAttributeTypes[attributeName]
		if !ok {
			attributeType = types.ScalarAttributeTypeS
		}
		attributeDefinitionMap[attributeName] = types.AttributeDefinition{
			AttributeName: aws.String(attributeName),
			AttributeType: attributeType,
		}
	}
	for _, k := range args.KeySchema {
		addAttribute(k.AttributeName)
	}

	var gsis = srv.getGSIs(args)
	for _, gsi := range args.GSIs {
		addAttribute(gsi.PartitionKey)
		if gsi.SortKey != "" {
			addAttribute(gsi.SortKey)
		}
	}

	var lsis = srv.getLsis(args, hashKey)
	for _, lsi := range args.LSIs {
		addAttribute(lsi.SortKey)
	}

	attributeDefinitionList := make([]types.AttributeDefinition, 0, len(attributeDefinitionMap))
	for _, ad := range attributeDefinitionMap {
		attributeDefinitionList = append(attributeDefinitionList, ad)
	}
	output, err := srv.db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:              aws.String(args.TableName),
		KeySchema:              keySchema,
		AttributeDefinitions:   attributeDefinitionList,
		BillingMode:            types.BillingMode(args.BillingMode),
		GlobalSecondaryIndexes: gsis,
		LocalSecondaryIndexes:  lsis,
		ProvisionedThroughput:  srv.getProvisionedThroughput(args.BillingMode, args.ReadCapacityUnits, args.WriteCapacityUnits),
	})
	if err != nil {
		srv.sendAuditLog("create_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("CreateTable %s failed: %v", args.TableName, err)), nil, nil
	}
	attributeDefinitions := srv.getAttributeDefinitions(output.TableDescription.AttributeDefinitions)
	srv.sendAuditLog("create_table", args.TableName, "", nil, nil)

	return srv.successResult(fmt.Sprintf("Successfully created table \"%s\"\n Attribute definitions: %s", args.TableName, strings.Join(attributeDefinitions, ", "))), nil, nil
}

func (srv *Server) updateTable(ctx context.Context, req *mcp.CallToolRequest, args *UpdateTableArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("UpdateTable: %v", err)), nil, nil
	}

	input := &dynamodb.UpdateTableInput{
		TableName: aws.String(args.TableName),
	}

	if args.BillingMode != "" {
		input.BillingMode = types.BillingMode(args.BillingMode)
	}

	if args.ProvisionedThroughput != nil && (args.ProvisionedThroughput.WriteCapacityUnits > 0 || args.ProvisionedThroughput.ReadCapacityUnits > 0 || args.BillingMode == string(types.BillingModeProvisioned)) {
		ru := args.ProvisionedThroughput.ReadCapacityUnits
		if ru == 0 {
			ru = 1
		}
		wu := args.ProvisionedThroughput.WriteCapacityUnits
		if wu == 0 {
			wu = 1
		}
		input.ProvisionedThroughput = &types.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(ru),
			WriteCapacityUnits: aws.Int64(wu),
		}
	}
    if len(args.GlobalSecondaryIndexUpdates) > 0 {
        // Filter out empty update structs where all actions are nil
        var realUpdates []types.GlobalSecondaryIndexUpdate
        for _, upd := range args.GlobalSecondaryIndexUpdates {
            if upd.Create != nil || upd.Update != nil || upd.Delete != nil {
                realUpdates = append(realUpdates, upd)
            }
        }
        if len(realUpdates) > 0 {
            input.GlobalSecondaryIndexUpdates = realUpdates
        }
    }

	if len(args.AttributeDefinitions) > 0 {
		var attriList []types.AttributeDefinition
		for _, attr := range args.AttributeDefinitions {
			attriList = append(attriList, types.AttributeDefinition{
				AttributeName: aws.String(attr.AttributeName),
				AttributeType: types.ScalarAttributeType(attr.AttributeType),
			})
		}
		input.AttributeDefinitions = attriList
	}

	output, err := srv.db.UpdateTable(ctx, input)
	if err != nil {
		srv.sendAuditLog("update_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("UpdateTable %s failed: %v", args.TableName, err)), nil, nil
	}
	srv.sendAuditLog("update_table", args.TableName, "", nil, nil)

	return srv.successResult(fmt.Sprintf("Successfully updated table \"%s\"\n Table status: %v", args.TableName, output.TableDescription.TableStatus)), nil, nil
}

func (srv *Server) deleteTable(ctx context.Context, req *mcp.CallToolRequest, args *DeleteTableArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("DeleteTable: %v", err)), nil, nil
	}
	_, err := srv.db.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(args.TableName),
	})
	if err != nil {
		srv.sendAuditLog("delete_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("DeleteTable %s failed: %v", args.TableName, err)), nil, nil
	}
	srv.sendAuditLog("delete_table", args.TableName, "", nil, nil)
	return srv.successResult(fmt.Sprintf("Successfully deleted table %s", args.TableName)), nil, nil
}

// readAuditLogs does not call sendAuditLog, so no change needed here.
func (srv *Server) readAuditLogs(ctx context.Context, req *mcp.CallToolRequest, args *ReadAuditLogsArgs) (*mcp.CallToolResult, any, error) {
	limit, _ := srv.guardrail.EnforceLimit(args.Limit)

	var startTime, endTime time.Time
	if args.StartTime != "" {
		var err error
		startTime, err = time.Parse(time.RFC3339, args.StartTime)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when parsing start time: %v", err)), nil, nil
		}
	}
	if args.EndTime != "" {
		var err error
		endTime, err = time.Parse(time.RFC3339, args.EndTime)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when parsing end time: %v", err)), nil, nil
		}
	}
	// Set defaults if times are zero
	if startTime.IsZero() {
		startTime = time.Unix(0, 0)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}
	if startTime.After(endTime) {
		return srv.errorResult(fmt.Sprintf("Error when parsing start time: start time must be before end time: %v - %v", startTime, endTime)), nil, nil
	}
	logs, err := srv.auditLog.ReadAuditHistory(limit, startTime, endTime)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error when reading audit logs: %v", err)), nil, nil
	}

	logsJson, _ := json.Marshal(logs)
	return srv.successResult(fmt.Sprintf("Successfully read %d audit logs: %s", len(logs), string(logsJson))), nil, nil
}

func (srv *Server) validateSchema(tableName string, av map[string]types.AttributeValue) *mcp.CallToolResult {
	if err := srv.guardrail.ValidateSchema(tableName, av); err != nil {
		return srv.errorResult(fmt.Sprintf("Error when validating schema for table %s: %v", tableName, err))
	}
	return nil
}

func (srv *Server) generateAuditEntry(operation string, tableName string, consumedCapacity float64, capacityType string, status string) audit.AuditEntry {
	msg := fmt.Sprintf("%s performed on table %s with status %s", operation, tableName, status)
	return audit.AuditEntry{
		Timestamp:             time.Now(),
		Operation:             operation,
		TableName:             tableName,
		User:                  srv.userID,
		CapacityUnitsConsumed: consumedCapacity,
		CapacityType:          capacityType,
		Status:                status,
		Message:               fmt.Sprintf("%s [user: %s, ARN: %s]", msg, srv.userID, srv.userARN),
	}
}

func (srv *Server) sendAuditLog(operation string, tableName string, capacityType string, consumedCapacity *types.ConsumedCapacity, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	var consumedCapacityUnits float64 = 0
	if consumedCapacity != nil && consumedCapacity.CapacityUnits != nil {
		consumedCapacityUnits = *consumedCapacity.CapacityUnits
	}
	srv.RecordActionLog(srv.auditLog, srv.generateAuditEntry(operation, tableName, consumedCapacityUnits, capacityType, status))
}

func (srv *Server) aggregateConsumedCapacity(ccList []types.ConsumedCapacity) *types.ConsumedCapacity {
	var total float64
	for _, cc := range ccList {
		if cc.CapacityUnits != nil {
			total += *cc.CapacityUnits
		}
	}

	return &types.ConsumedCapacity{
		CapacityUnits: &total,
	}
}

func (srv *Server) successResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: message,
			},
		},
	}
}

func (srv *Server) errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: message,
			},
		},
		IsError: true,
	}
}

func (srv *Server) getProvisionedThroughput(billingMode string, readUnits, writeUnits int64) *types.ProvisionedThroughput {
	if billingMode == string(types.BillingModeProvisioned) {
		ru := int64(1)
		wu := int64(1)
		if readUnits > 0 {
			ru = readUnits
		}
		if writeUnits > 0 {
			wu = writeUnits
		}
		return &types.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(ru),
			WriteCapacityUnits: aws.Int64(wu),
		}
	}
	return nil
}

func (srv *Server) getKeySchema(keySchema []KeySchema) ([]types.KeySchemaElement, string) {
	var keySchemaElements []types.KeySchemaElement
	var hashKey string
	for _, ks := range keySchema {
		if ks.KeyType == string(types.KeyTypeHash) {
			hashKey = ks.AttributeName
			keySchemaElements = append(keySchemaElements, types.KeySchemaElement{
				AttributeName: aws.String(ks.AttributeName),
				KeyType:       types.KeyTypeHash,
			})
		} else {
			keySchemaElements = append(keySchemaElements, types.KeySchemaElement{
				AttributeName: aws.String(ks.AttributeName),
				KeyType:       types.KeyTypeRange,
			})
		}
	}

	return keySchemaElements, hashKey
}

func (srv *Server) getGSIs(args *CreateTableArgs) []types.GlobalSecondaryIndex {
	var gsisList []types.GlobalSecondaryIndex
	for _, gsi := range args.GSIs {
		gsiKeySchema := []types.KeySchemaElement{
			{
				AttributeName: aws.String(gsi.PartitionKey),
				KeyType:       types.KeyTypeHash,
			},
		}
		if gsi.SortKey != "" {
			gsiKeySchema = append(gsiKeySchema, types.KeySchemaElement{
				AttributeName: aws.String(gsi.SortKey),
				KeyType:       types.KeyTypeRange,
			})
		}

		gsisList = append(gsisList, types.GlobalSecondaryIndex{
			IndexName: aws.String(gsi.IndexName),
			KeySchema: gsiKeySchema,
			Projection: &types.Projection{
				ProjectionType: types.ProjectionTypeAll,
			},
			ProvisionedThroughput: srv.getProvisionedThroughput(args.BillingMode, args.ReadCapacityUnits, args.WriteCapacityUnits),
		})
	}

	return gsisList
}

func (srv *Server) getLsis(args *CreateTableArgs, hashKey string) []types.LocalSecondaryIndex {
	var lsisList []types.LocalSecondaryIndex
	for _, lsi := range args.LSIs {
		lsiKeySchema := []types.KeySchemaElement{
			{
				AttributeName: aws.String(hashKey),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String(lsi.SortKey),
				KeyType:       types.KeyTypeRange,
			},
		}

		lsisList = append(lsisList, types.LocalSecondaryIndex{
			IndexName: aws.String(lsi.IndexName),
			KeySchema: lsiKeySchema,
			Projection: &types.Projection{
				ProjectionType: types.ProjectionTypeAll,
			},
		})
	}

	return lsisList
}

func (srv *Server) getAttributeDefinitions(adList []types.AttributeDefinition) []string {
	var attributeDefinitions []string
	for _, ad := range adList {
		attributeDefinitions = append(attributeDefinitions, fmt.Sprintf("%s (%s)", *ad.AttributeName, ad.AttributeType))
	}
	return attributeDefinitions
}
