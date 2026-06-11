package server

import "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

type ListTablesArgs struct {
}

type DescribeTableArgs struct {
	TableName string `json:"tableName"`
}

type DeleteTableArgs struct {
	TableName    string `json:"tableName"`
	Confirmation *bool  `json:"confirmation"`
}

type ScanTableArgs struct {
	TableName                 string            `json:"tableName"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	FilterExpression          string            `json:"filterExpression"`
	ProjectionExpression      string            `json:"projectionExpression"`
	IndexName                 string            `json:"indexName"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	Limit                     int32             `json:"limit"`
	ExclusiveStartKey         map[string]any `json:"exclusiveStartKey"`
	ConsistentRead            *bool          `json:"consistentRead"`
	Confirmation              *bool          `json:"confirmation"`
}

type PutItemArgs struct {
	TableName    string         `json:"tableName"`
	Item         map[string]any `json:"item"`
	Confirmation *bool          `json:"confirmation"`
}

type QueryTableArgs struct {
	TableName                 string            `json:"tableName"`
	IndexName                 string            `json:"indexName"`
	KeyConditionExpression    string            `json:"keyConditionExpression"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	Limit                     int32             `json:"limit"`
	ExclusiveStartKey         map[string]any    `json:"exclusiveStartKey"`
	ConsistentRead            *bool             `json:"consistentRead"`
}

type BatchPutItemsArgs struct {
	TableName string           `json:"tableName"`
	Items     []map[string]any `json:"items"`
	Confirmation              *bool          `json:"confirmation"`
}

type BatchDeleteItemsArgs struct {
	TableName    string           `json:"tableName"`
	Keys         []map[string]any `json:"keys"`
	Confirmation *bool            `json:"confirmation"`
}

type DeleteItemArgs struct {
	TableName    string         `json:"tableName"`
	Key          map[string]any `json:"key"`
	Confirmation *bool          `json:"confirmation"`
}

type GetItemArgs struct {
	TableName string         `json:"tableName"`
	Key       map[string]any `json:"key"`
}

type UpdateItemArgs struct {
	TableName                 string            `json:"tableName"`
	Key                       map[string]any    `json:"key"`
	UpdateExpression          string            `json:"updateExpression"`
	ConditionExpression       string            `json:"conditionExpression"`
	ReturnValue               string            `json:"returnValues"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	Confirmation              *bool             `json:"confirmation"`
}

type BatchGetItemArgs struct {
	TableName    string           `json:"tableName"`
	Keys         []map[string]any `json:"keys"`
	Confirmation *bool            `json:"confirmation"`
}

type ReadAuditLogsArgs struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Limit     int32  `json:"limit"`
}

type CreateTableArgs struct {
	TableName            string                `json:"tableName"`
	KeySchema            []KeySchema           `json:"keySchema"`
	AttributeDefinitions []AttributeDefinition `json:"attributeDefinitions"`
	BillingMode          string                `json:"billingMode"`
	GSIs                 []GSI                 `json:"gsis"`
	LSIs                 []LSI                 `json:"lsis"`
	ReadCapacityUnits    int64                 `json:"readCapacityUnits,omitempty"`
	WriteCapacityUnits   int64                 `json:"writeCapacityUnits,omitempty"`
	Tags                 []Tag                 `json:"tags,omitempty"`
	Confirmation         *bool                 `json:"confirmation"`
}

type UpdateTableTTLArgs struct {
	TableName   string `json:"tableName"`
	AttributeName string `json:"attributeName"`
	Enabled     bool   `json:"enabled"`
}

type Tag struct {
	Key   string   `json:"key"`
	Value []string `json:"value"`
}

type UpdateTableArgs struct {
	TableName                   string                             `json:"tableName"`
	GlobalSecondaryIndexUpdates []types.GlobalSecondaryIndexUpdate `json:"globalSecondaryIndexUpdates"`
	BillingMode                 string                             `json:"billingMode"`
	ProvisionedThroughput       *ProvisionedThroughput             `json:"provisionedThroughput,omitempty"`
	AttributeDefinitions        []AttributeDefinition              `json:"attributeDefinitions,omitempty"`
	Confirmation                *bool                              `json:"confirmation"`
}

type GSI struct {
	IndexName      string `json:"indexName"`
	PartitionKey   string `json:"partitionKey"`
	SortKey        string `json:"sortKey"`
	ProjectionType string `json:"projectionType"`
}

type LSI struct {
	IndexName      string `json:"indexName"`
	SortKey        string `json:"sortKey"`
}

type KeySchema struct {
	AttributeName string `json:"attributeName"`
	KeyType       string `json:"keyType"`
}

type ProvisionedThroughput struct {
	ReadCapacityUnits  int64 `json:"readCapacityUnits"`
	WriteCapacityUnits int64 `json:"writeCapacityUnits"`
}

type AttributeDefinition struct {
	AttributeName string `json:"attributeName"`
	AttributeType string `json:"attributeType"`
}

const batchSize = 25
const defaultLimit = 20
