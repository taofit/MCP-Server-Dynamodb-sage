package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/metrics"
	"encoding/json"
	"fmt"
	"log"
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

	qi := &dynamodb.QueryInput{
		TableName:                 &args.TableName,
		KeyConditionExpression:    &args.KeyConditionExpression,
		ExpressionAttributeNames:  args.ExpressionAttributeNames,
		ExpressionAttributeValues: attributevalues,
		Limit:                     &limit,
		ExclusiveStartKey:         startKey,
		ReturnConsumedCapacity:    types.ReturnConsumedCapacityTotal,
		ConsistentRead:            args.ConsistentRead,
	}
	if args.IndexName != "" {
		qi.IndexName = &args.IndexName
	}

	output, err := instrumentDynamoDB("query_table", args.TableName, func() (*dynamodb.QueryOutput, error) {
		return srv.db.Query(ctx, qi)
	})
	recordConsumedCapacity("query_table", args.TableName, "RCU", output.ConsumedCapacity)

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
	scrubbedItems := srv.guardrail.ScrubItems("query_table", args.TableName, items)
	for i, item := range scrubbedItems {
		itemJSON, _ := json.Marshal(item)
		itemsText += fmt.Sprintf("\n[%d] %s", i+1, string(itemJSON))
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
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(err.Error()), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table item cannot be put", err)), nil, nil
	}

	if len(args.Item) == 0 {
		return srv.errorResult(fmt.Sprintf("Item to put into table %s cannot be empty", args.TableName)), nil, nil
	}
	// Convert the plain Go map into a map of DynamoDB AttributeValues
	av, err := attributevalue.MarshalMap(args.Item)
	if err != nil {
		return srv.errorResult(fmt.Sprintf("Error marshaling item: %v", err)), nil, nil
	}
	if res := srv.validateSchema(args.TableName, av); res != nil {
		return res, nil, nil
	}

	output, err := instrumentDynamoDB("put_item", args.TableName, func() (*dynamodb.PutItemOutput, error) {
		return srv.db.PutItem(ctx, &dynamodb.PutItemInput{
			TableName:              &args.TableName,
			Item:                   av,
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		})
	})
	recordConsumedCapacity("put_item", args.TableName, "WCU", output.ConsumedCapacity)
	if err != nil {
		var cc *types.ConsumedCapacity
		if output != nil {
			cc = output.ConsumedCapacity
		}
		srv.sendAuditLog("put_item", args.TableName, "WCU", cc, err)

		return srv.errorResult(fmt.Sprintf("Error when putting item: %v", err)), nil, nil
	}
	srv.sendAuditLog("put_item", args.TableName, "WCU", output.ConsumedCapacity, nil)

	srv.sendMutationNotification(args.TableName, "put_item", "success", "Item put successfully")
	return srv.successResult(fmt.Sprintf("Successfully put item into table %s", args.TableName)), nil, nil
}

func (srv *Server) listTables(ctx context.Context, req *mcp.CallToolRequest, args *ListTablesArgs) (*mcp.CallToolResult, any, error) {
	var allTables []string
	paginator := dynamodb.NewListTablesPaginator(srv.db, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			srv.sendAuditLog("list_tables", "", "", nil, err)
			return srv.errorResult(fmt.Sprintf("Error when listing tables: %v", err)), nil, nil
		}
		allTables = append(allTables, out.TableNames...)
	}

	tables := strings.Join(allTables, ", ")
	if tables == "" {
		tables = "(no tables found)"
	}
	srv.sendAuditLog("list_tables", "", "", nil, nil)

	return srv.successResult(fmt.Sprintf("DynamoDB Tables: %s", tables)), nil, nil
}

func (srv *Server) describeTable(ctx context.Context, req *mcp.CallToolRequest, args *DescribeTableArgs) (*mcp.CallToolResult, any, error) {
	out, err := instrumentDynamoDB("describe_table", args.TableName, func() (*dynamodb.DescribeTableOutput, error) {
		return srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: &args.TableName,
		})
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
	if len(args.ExpressionAttributeValues) > 0 {
		var err error
		input.ExpressionAttributeValues, err = attributevalue.MarshalMap(args.ExpressionAttributeValues)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshaling expression attribute values: %v", err)), nil, nil
		}
	}
	if len(args.ExpressionAttributeNames) > 0 {
		input.ExpressionAttributeNames = make(map[string]string)
		for k, v := range args.ExpressionAttributeNames {
			input.ExpressionAttributeNames[k] = v
		}
	}
	if args.IndexName != "" {
		input.IndexName = &args.IndexName
	}
	if args.ExclusiveStartKey != nil {
		startKey, err := attributevalue.MarshalMap(args.ExclusiveStartKey)
		if err != nil {
			return srv.errorResult(fmt.Sprintf("Error when marshaling exclusive start key: %v", err)), nil, nil
		}
		input.ExclusiveStartKey = startKey
	}
	if args.ConsistentRead != nil {
		input.ConsistentRead = args.ConsistentRead
	}
	out, err := instrumentDynamoDB("scan_table", args.TableName, func() (*dynamodb.ScanOutput, error) {
		return srv.db.Scan(ctx, input)
	})
	recordConsumedCapacity("scan_table", args.TableName, "RCU", out.ConsumedCapacity)

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
	scrubbedItems := srv.guardrail.ScrubItems("scan_table", args.TableName, items)
	for i, item := range scrubbedItems {
		itemJSON, _ := json.Marshal(item)
		itemsText += fmt.Sprintf("\n[%d] %s", i+1, string(itemJSON))
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
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(err.Error()), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table items cannot be put", err)), nil, nil
	}

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
			output, err := instrumentDynamoDB("batch_write_item", args.TableName, func() (*dynamodb.BatchWriteItemOutput, error) {
				return srv.db.BatchWriteItem(ctx, input)
			})
			if output != nil && output.ConsumedCapacity != nil {
				for _, cc := range output.ConsumedCapacity {
					recordConsumedCapacity("batch_write_item", args.TableName, "WCU", &cc)
				}
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

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table items cannot be deleted", err)), nil, nil
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
			output, err := instrumentDynamoDB("batch_write_item", args.TableName, func() (*dynamodb.BatchWriteItemOutput, error) {
				return srv.db.BatchWriteItem(ctx, input)
			})
			if output != nil && output.ConsumedCapacity != nil {
				for _, cc := range output.ConsumedCapacity {
					recordConsumedCapacity("batch_write_item", args.TableName, "WCU", &cc)
				}
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

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if len(args.Key) == 0 {
		return srv.errorResult(fmt.Sprintf("Key is required for deleting an item from table %s", args.TableName)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table item cannot be deleted", err)), nil, nil
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

	output, err := instrumentDynamoDB("delete_item", args.TableName, func() (*dynamodb.DeleteItemOutput, error) {
		return srv.db.DeleteItem(ctx, input)
	})
	recordConsumedCapacity("delete_item", args.TableName, "WCU", output.ConsumedCapacity)
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
		keyJSON, _ := json.Marshal(args.Key)
		return srv.errorResult(fmt.Sprintf("Item with key %s not found in table %s", string(keyJSON), args.TableName)), nil, nil
	}

	attributes := map[string]any{}
	attributevalue.UnmarshalMap(output.Attributes, &attributes)

	scrubbedItems := srv.guardrail.ScrubItems("delete_item", args.TableName, []map[string]any{attributes})
	itemJSON, _ := json.Marshal(scrubbedItems[0])
	keyJSON, _ := json.Marshal(args.Key)
	srv.sendMutationNotification(args.TableName, "delete_item", "success", fmt.Sprintf("Successfully deleted item %s from table: %s. Attributes: %s", string(keyJSON), args.TableName, string(itemJSON)))

	return srv.successResult(fmt.Sprintf("Successfully deleted item %s from table: %s. Attributes: %s", string(keyJSON), args.TableName, string(itemJSON))), nil, nil
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

	output, err := instrumentDynamoDB("get_item", args.TableName, func() (*dynamodb.GetItemOutput, error) {
		return srv.db.GetItem(ctx, input)
	})
	recordConsumedCapacity("get_item", args.TableName, "RCU", output.ConsumedCapacity)
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
	keyJSON, _ := json.Marshal(args.Key)

	if len(item) == 0 {
		return srv.errorResult(fmt.Sprintf("Item with key %s not found in table %s", string(keyJSON), args.TableName)), nil, nil
	}

	scrubbedItems := srv.guardrail.ScrubItems("get_item", args.TableName, []map[string]any{item})
	scrubbedItem := scrubbedItems[0]
	itemJSON, _ := json.Marshal(scrubbedItem)

	srv.sendAuditLog("get_item", args.TableName, "RCU", output.ConsumedCapacity, nil)

	return srv.successResult(fmt.Sprintf("Successfully got item with key %s from table %s: %s", string(keyJSON), args.TableName, string(itemJSON))), nil, nil
}

func (srv *Server) updateItem(ctx context.Context, req *mcp.CallToolRequest, args *UpdateItemArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table item cannot be updated", err)), nil, nil
	}

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

	output, err := instrumentDynamoDB("update_item", args.TableName, func() (*dynamodb.UpdateItemOutput, error) {
		return srv.db.UpdateItem(ctx, input)
	})
	recordConsumedCapacity("update_item", args.TableName, "WCU", output.ConsumedCapacity)
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
	var scrubbedItem map[string]any
	if len(output.Attributes) != 0 {
		attributevalue.UnmarshalMap(output.Attributes, &attributes)
		scrubbedItems := srv.guardrail.ScrubItems("update_item", args.TableName, []map[string]any{attributes})
		scrubbedItem = scrubbedItems[0]
	}

	keyJSON, _ := json.Marshal(args.Key)
	attributesMsg := ""
	if len(scrubbedItem) > 0 {
		scrubbedAttributeJSON, _ := json.Marshal(scrubbedItem)
		attributesMsg = fmt.Sprintf(", Attributes: %s", string(scrubbedAttributeJSON))
	}
	srv.sendMutationNotification(args.TableName, "update_item", "success", fmt.Sprintf("Successfully updated item %v from table %s%s", string(keyJSON), args.TableName, attributesMsg))

	return srv.successResult(fmt.Sprintf("Successfully updated item %v from table %s%s", string(keyJSON), args.TableName, attributesMsg)), nil, nil
}

func (srv *Server) batchGetItems(ctx context.Context, req *mcp.CallToolRequest, args *BatchGetItemArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Keys) == 0 {
		return srv.errorResult(fmt.Sprintf("Error: No keys provided for batch get from table %s", args.TableName)), nil, nil
	}

	keys := make([]map[string]types.AttributeValue, 0, len(args.Keys))
	existingKeys := make(map[string]bool)

	for _, key := range args.Keys {
		keyJSON, _ := json.Marshal(key)
		keyStr := string(keyJSON)
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
			output, err := instrumentDynamoDB("batch_get_item", args.TableName, func() (*dynamodb.BatchGetItemOutput, error) {
				return srv.db.BatchGetItem(ctx, input)
			})
			if output != nil && output.ConsumedCapacity != nil {
				for _, cc := range output.ConsumedCapacity {
					recordConsumedCapacity("batch_get_item", args.TableName, "RCU", &cc)
				}
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
		failedJSON, _ := json.Marshal(failedList)
		unprocessedMsg = fmt.Sprintf(", Warning: %d keys were not processed (provisioned throughput exceeded). Failed keys: %s", len(failedList), string(failedJSON))
	}
	scrubbedItems := []map[string]any{}
	for _, item := range outputResponse {
		scrubbedItem := map[string]any{}
		attributevalue.UnmarshalMap(item, &scrubbedItem)
		scrubbedItems = append(scrubbedItems, scrubbedItem)
	}
	scrubbedItems = srv.guardrail.ScrubItems("batch_get_items", args.TableName, scrubbedItems)

	itemStrings := []string{}
	for _, item := range scrubbedItems {
		itemTrans, _ := json.Marshal(item)
		itemStrings = append(itemStrings, string(itemTrans))
	}

	itemsJSON, _ := json.Marshal(itemStrings)
	return srv.successResult(fmt.Sprintf("Successfully batch get %d items from table %s: %s%s", len(scrubbedItems), args.TableName, string(itemsJSON), unprocessedMsg)), nil, nil
}

func (srv *Server) createOptimizedTable(ctx context.Context, req *mcp.CallToolRequest, args *CreateOptimizedTableArgs) (*mcp.CallToolResult, any, error) {
	keySchema, hashKey := srv.getKeySchema(args.KeySchema)

	if len(args.LSIs) > 0 {
		hasRangeKey := false
		for _, k := range args.KeySchema {
			if k.KeyType == string(types.KeyTypeRange) {
				hasRangeKey = true
				break
			}
		}
		if !hasRangeKey {
			return srv.errorResult("LSI requires a composite primary key (HASH + RANGE). Add a sort key (keyType: \"RANGE\") to keySchema when using LSIs."), nil, nil
		}
	}

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

	tags := toDynamoTags(args.Tags)

	attributeDefinitionList := make([]types.AttributeDefinition, 0, len(attributeDefinitionMap))
	for _, ad := range attributeDefinitionMap {
		attributeDefinitionList = append(attributeDefinitionList, ad)
	}
	output, err := instrumentDynamoDB("create_table", args.TableName, func() (*dynamodb.CreateTableOutput, error) {
		return srv.db.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName:              aws.String(args.TableName),
			KeySchema:              keySchema,
			AttributeDefinitions:   attributeDefinitionList,
			BillingMode:            types.BillingMode(args.BillingMode),
			GlobalSecondaryIndexes: gsis,
			LocalSecondaryIndexes:  lsis,
			ProvisionedThroughput:  srv.getProvisionedThroughput(args.BillingMode, args.ReadCapacityUnits, args.WriteCapacityUnits),
			Tags:                   tags,
		})
	})
	if err != nil {
		srv.sendAuditLog("create_optimized_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("CreateTable %s failed: %v", args.TableName, err)), nil, nil
	}
	attributeDefinitions := srv.getAttributeDefinitions(output.TableDescription.AttributeDefinitions)
	srv.sendAuditLog("create_optimized_table", args.TableName, "", nil, nil)

	return srv.successResult(fmt.Sprintf("Successfully created table \"%s\"\n Attribute definitions: %s", args.TableName, strings.Join(attributeDefinitions, ", "))), nil, nil
}

func (srv *Server) updateTable(ctx context.Context, req *mcp.CallToolRequest, args *UpdateTableArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("UpdateTable: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table cannot be updated", err)), nil, nil
	}

	input := &dynamodb.UpdateTableInput{
		TableName: aws.String(args.TableName),
	}

	if args.BillingMode != "" {
		input.BillingMode = types.BillingMode(args.BillingMode)
	}

	if args.ProvisionedThroughput != nil && (args.ProvisionedThroughput.WriteCapacityUnits >= 0 || args.ProvisionedThroughput.ReadCapacityUnits >= 0 || args.BillingMode == string(types.BillingModeProvisioned)) {
		ru := args.ProvisionedThroughput.ReadCapacityUnits
		wu := args.ProvisionedThroughput.WriteCapacityUnits
		if err := srv.guardrail.ValidateCapacityUnits(ru); err != nil {
			return srv.errorResult(fmt.Sprintf("UpdateTable: %v", err)), nil, nil
		}
		if err := srv.guardrail.ValidateCapacityUnits(wu); err != nil {
			return srv.errorResult(fmt.Sprintf("UpdateTable: %v", err)), nil, nil
		}
		if ru == 0 {
			ru = 1
		}
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

	output, err := instrumentDynamoDB("update_table", args.TableName, func() (*dynamodb.UpdateTableOutput, error) {
		return srv.db.UpdateTable(ctx, input)
	})
	if err != nil {
		srv.sendAuditLog("update_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("Failed to update table %s: %v", args.TableName, err)), nil, nil
	}

	tags := toDynamoTags(args.Tags)
	if len(tags) > 0 {
		if output.TableDescription == nil || output.TableDescription.TableArn == nil {
			return srv.errorResult(fmt.Sprintf("UpdateTable %s did not return a table ARN", args.TableName)), nil, nil
		}
		if err := srv.tagTable(ctx, args.TableName, *output.TableDescription.TableArn, tags); err != nil {
			srv.sendAuditLog("update_table", args.TableName, "", nil, err)
			return srv.errorResult(fmt.Sprintf("UpdateTable %s succeeded, but TagResource failed: %v", args.TableName, err)), nil, nil
		}
	}

	srv.sendAuditLog("update_table", args.TableName, "", nil, nil)

	if len(tags) > 0 {
		srv.sendMutationNotification(args.TableName, "update_table", "success", fmt.Sprintf("Successfully updated table \"%s\"\n Table status: %v\n Tags applied: %s", args.TableName, output.TableDescription.TableStatus, tagSummary(tags)))
		return srv.successResult(fmt.Sprintf("Successfully updated table \"%s\"\n Table status: %v\n Tags applied: %s", args.TableName, output.TableDescription.TableStatus, tagSummary(tags))), nil, nil
	}
	srv.sendMutationNotification(args.TableName, "update_table", "success", fmt.Sprintf("Successfully updated table \"%s\"\n Table status: %v", args.TableName, output.TableDescription.TableStatus))

	return srv.successResult(fmt.Sprintf("Successfully updated table \"%s\"\n Table status: %v", args.TableName, output.TableDescription.TableStatus)), nil, nil
}

func toDynamoTags(tags []Tag) []types.Tag {
	dynamoTags := make([]types.Tag, 0, len(tags))
	for _, tag := range tags {
		dynamoTags = append(dynamoTags, types.Tag{
			Key:   aws.String(tag.Key),
			Value: aws.String(strings.Join(tag.Value, ",")),
		})
	}
	return dynamoTags
}

func (srv *Server) tagTable(ctx context.Context, tableName, tableArn string, tags []types.Tag) error {
	if len(tags) == 0 {
		return nil
	}
	if tableArn == "" {
		return fmt.Errorf("table ARN is required to tag DynamoDB table")
	}
	_, err := instrumentDynamoDB("tag_resource", tableName, func() (*dynamodb.TagResourceOutput, error) {
		return srv.db.TagResource(ctx, &dynamodb.TagResourceInput{
			ResourceArn: aws.String(tableArn),
			Tags:        tags,
		})
	})
	if err != nil {
		return fmt.Errorf("tag table %s: %w", tableName, err)
	}
	return nil
}

func tagSummary(tags []types.Tag) string {
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		items = append(items, fmt.Sprintf("%s=%s", *tag.Key, *tag.Value))
	}
	return strings.Join(items, ", ")
}

func (srv *Server) deleteTable(ctx context.Context, req *mcp.CallToolRequest, args *DeleteTableArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("DeleteTable: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table cannot be deleted", err)), nil, nil
	}
	_, err := instrumentDynamoDB("delete_table", args.TableName, func() (*dynamodb.DeleteTableOutput, error) {
		return srv.db.DeleteTable(ctx, &dynamodb.DeleteTableInput{
			TableName: aws.String(args.TableName),
		})
	})
	if err != nil {
		srv.sendAuditLog("delete_table", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("DeleteTable %s failed: %v", args.TableName, err)), nil, nil
	}
	srv.sendAuditLog("delete_table", args.TableName, "", nil, nil)
	srv.sendMutationNotification(args.TableName, "delete_table", "success", fmt.Sprintf("Successfully deleted table %s", args.TableName))

	return srv.successResult(fmt.Sprintf("Successfully deleted table %s", args.TableName)), nil, nil
}

func (srv *Server) updateTableTTL(ctx context.Context, req *mcp.CallToolRequest, args *UpdateTableTTLArgs) (*mcp.CallToolResult, any, error) {
	if err := srv.guardrail.ValidateProtectedTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("UpdateTable: %v", err)), nil, nil
	}

	if err := srv.guardrail.ValidateReadOnlyTable(args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v", err)), nil, nil
	}

	if err := srv.validateProtectedTag(ctx, args.TableName); err != nil {
		return srv.errorResult(fmt.Sprintf("Validation error: %v; table cannot be modified", err)), nil, nil
	}

	output, err := instrumentDynamoDB("describe_table", args.TableName, func() (*dynamodb.DescribeTableOutput, error) {
		return srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(args.TableName),
		})
	})
	if err != nil {
		return srv.errorResult(fmt.Sprintf("UpdateTableTTL %s failed: %v", args.TableName, err)), nil, nil
	}
	var attributeName string
	for _, attr := range output.Table.AttributeDefinitions {
		if *attr.AttributeName == args.AttributeName {
			attributeName = *attr.AttributeName
			break
		}
	}
	if attributeName == "" {
		return srv.errorResult(fmt.Sprintf("UpdateTableTTL: Attribute %s not found in table %s", args.AttributeName, args.TableName)), nil, nil
	}

	input := &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(args.TableName),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String(attributeName),
			Enabled:       aws.Bool(args.Enabled),
		},
	}

	_, err = instrumentDynamoDB("update_time_to_live", args.TableName, func() (*dynamodb.UpdateTimeToLiveOutput, error) {
		return srv.db.UpdateTimeToLive(ctx, input)
	})
	if err != nil {
		srv.sendAuditLog("update_table_ttl", args.TableName, "", nil, err)
		return srv.errorResult(fmt.Sprintf("UpdateTableTTL %s failed: %v", args.TableName, err)), nil, nil
	}
	ttlStatus := "Unknown"
	ttlOutput, _ := instrumentDynamoDB("describe_time_to_live", args.TableName, func() (*dynamodb.DescribeTimeToLiveOutput, error) {
		return srv.db.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
			TableName: aws.String(args.TableName),
		})
	})
	if ttlOutput != nil && ttlOutput.TimeToLiveDescription != nil {
		ttlStatus = string(ttlOutput.TimeToLiveDescription.TimeToLiveStatus)
	}
	srv.sendAuditLog("update_table_ttl", args.TableName, "", nil, nil)
	return srv.successResult(fmt.Sprintf("Successfully updated table %s TTL status to %s", args.TableName, ttlStatus)), nil, nil
}

func (srv *Server) getJobResult(ctx context.Context, req *mcp.CallToolRequest, args *GetJobResultArgs) (*mcp.CallToolResult, any, error) {
	jobResult, ok := srv.jobStorage.Load(args.JobID)
	defer func() {
		srv.jobStorage.Delete(args.JobID)
		metrics.JobStoragePending.Dec()
	}()
	if !ok {
		return srv.errorResult(fmt.Sprintf("Job %s not found", args.JobID)), nil, nil
	}
	jr := jobResult.(*JobResult)
	select {
	case <-ctx.Done():
		return srv.errorResult("Context cancelled"), nil, nil
	case <-jr.Done:
		if jr.Error != nil {
			return srv.errorResult(fmt.Sprintf("Job %s failed: %s", args.JobID, jr.Error.Error())), nil, nil
		}
		return jr.Result, nil, nil
	}
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

	logsJSON, _ := json.Marshal(logs)
	return srv.successResult(fmt.Sprintf("Successfully read %d audit logs: %s", len(logs), string(logsJSON))), nil, nil
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

func status(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

func instrumentDynamoDB[T any](operation, table string, fn func() (T, error)) (T, error) {
	start := time.Now()
	output, err := fn()
	dur := time.Since(start).Seconds()
	st := status(err)
	metrics.DynamoDBOperationDurationSeconds.WithLabelValues(operation, table, st).Observe(dur)
	metrics.DynamoDBOperationTotal.WithLabelValues(operation, table, st).Inc()
	return output, err
}

func instrumentHeavyJob(operation string, startedAt time.Time, err error) {
	dur := time.Since(startedAt).Seconds()
	st := status(err)
	metrics.AsyncJobDurationSeconds.WithLabelValues(operation, st).Observe(dur)
	metrics.AsyncJobsTotal.WithLabelValues(operation, st).Inc()
}

func recordConsumedCapacity(operation, table, capacityType string, cc *types.ConsumedCapacity) {
	if cc != nil && cc.CapacityUnits != nil {
		metrics.DynamoDBConsumedCapacityTotal.WithLabelValues(operation, table, capacityType).Add(*cc.CapacityUnits)
	}
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

func (srv *Server) getGSIs(args *CreateOptimizedTableArgs) []types.GlobalSecondaryIndex {
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
				ProjectionType: types.ProjectionType(gsi.ProjectionType),
			},
			ProvisionedThroughput: srv.getProvisionedThroughput(args.BillingMode, args.ReadCapacityUnits, args.WriteCapacityUnits),
		})
	}

	return gsisList
}

func (srv *Server) getLsis(args *CreateOptimizedTableArgs, hashKey string) []types.LocalSecondaryIndex {
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

func (srv *Server) validateProtectedTag(ctx context.Context, tableName string) error {
	desc, err := instrumentDynamoDB("describe_table", tableName, func() (*dynamodb.DescribeTableOutput, error) {
		return srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
	})
	if err != nil {
		return fmt.Errorf("error when describing table: %v", err)
	}
	tags, err := instrumentDynamoDB("list_tags_of_resource", tableName, func() (*dynamodb.ListTagsOfResourceOutput, error) {
		return srv.db.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{
			ResourceArn: desc.Table.TableArn,
		})
	})
	if err != nil {
		return fmt.Errorf("error when listing tags of resource: %v", err)
	}

	for _, tag := range tags.Tags {
		var protectedTagValues = srv.guardrail.GetTags(*tag.Key)
		if len(protectedTagValues) == 0 {
			continue
		}
		for _, protectedTagValue := range protectedTagValues {
			if protectedTagValue == *tag.Value {
				return fmt.Errorf("protected tag %s:%s exists for table %s", *tag.Key, *tag.Value, tableName)
			}
		}
	}

	return nil
}

func (srv *Server) processHeavyOp(key string, payload []byte) error {
	payloadResult, err := srv.notificationService.ParsePayload(payload)
	if err != nil {
		srv.notificationService.SendNotification("unknown", "error", "", "", err.Error())
		return err
	}
	jobResult, ok := srv.jobStorage.Load(key)
	if !ok {
		log.Printf("job not found: %s", key)
		return nil
	}

	jr := jobResult.(*JobResult)
	srv.executeJobOp(jr, payload)

	if jr.Error != nil {
		srv.notificationService.SendNotification(payloadResult.TableName, "error", "", "", jr.Error.Error())
		return jr.Error
	}
	srv.notificationService.SendNotification(payloadResult.TableName, "success", payloadResult.Operation, payloadResult.InputHash, srv.notificationService.ConstructMessage(payloadResult))

	return nil
}

func (srv *Server) processHeavyOpForQueue(key string, payload []byte) error {
	jobResult, ok := srv.jobStorage.Load(key)
	if !ok {
		return fmt.Errorf("job not found: %s", key)
	}
	jr := jobResult.(*JobResult)
	srv.executeJobOp(jr, payload)
	if jr.Error != nil {
		return jr.Error
	}
	return nil
}

func (srv *Server) executeJobOp(jr *JobResult, payload []byte) {
	jobPayload := struct {
		Input     json.RawMessage `json:"input"`
		Operation string          `json:"operation"`
	}{}
	jobPayload.Operation = "unknown"
	defer func() {
		if jr != nil && jr.Done != nil {
			instrumentHeavyJob(jobPayload.Operation, jr.StartedAt, jr.Error)
			close(jr.Done)
		}
	}()
	if err := json.Unmarshal(payload, &jobPayload); err != nil {
		jr.Error = fmt.Errorf("failed to unmarshal job payload: %v", err)
		return
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	switch jobPayload.Operation {
	case "batch_put_items":
		var input BatchPutItemsArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse batch_put_items args: %v", err)
		} else {
			result, _, err := srv.batchPutItems(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute batch_put_items: %v", err)
			} else {
				jr.Result = result
			}
		}
	case "batch_delete_items":
		var input BatchDeleteItemsArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse batch_delete_items args: %v", err)
		} else {
			result, _, err := srv.batchDeleteItems(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute batch_delete_items: %v", err)
			} else {
				jr.Result = result
			}
		}
	case "create_optimized_table":
		var input CreateOptimizedTableArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse create_optimized_table args: %v", err)
		} else {
			result, _, err := srv.createOptimizedTable(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute create_optimized_table: %v", err)
			} else {
				jr.Result = result
			}
		}
	default:
		jr.Error = fmt.Errorf("unknown operation: %s", jobPayload.Operation)
	}
}
