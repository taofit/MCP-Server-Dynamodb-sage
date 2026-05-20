package server

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
	TableName                 string            `json:"tableName"`
	KeyConditionExpression    string            `json:"keyConditionExpression"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	Limit                     int32             `json:"limit"`
	ExclusiveStartKey         map[string]any    `json:"exclusiveStartKey"`
}

type BatchPutItemsArgs struct {
	TableName string           `json:"tableName"`
	Items     []map[string]any `json:"items"`
}

type BatchDeleteItemsArgs struct {
	TableName string           `json:"tableName"`
	Keys      []map[string]any `json:"keys"`
}

type DeleteItemArgs struct {
	TableName string         `json:"tableName"`
	Key       map[string]any `json:"key"`
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
}

type BatchGetItemArgs struct {
	TableName string           `json:"tableName"`
	Keys      []map[string]any `json:"keys"`
}

type ReadAuditLogsArgs struct {
	Limit int `json:"limit"`
}

const batchSize = 25
