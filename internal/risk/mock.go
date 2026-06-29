package risk

import (
	"context"
	"dynamodb-sage/internal/engine"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// mockDynamoDB implements the minimal methods used by RiskAnalyzer for testing.
type mockDynamoDB struct {
	DescribeTableOut      *dynamodb.DescribeTableOutput
	GetItemOut            *dynamodb.GetItemOutput
	BatchGetItemOut       *dynamodb.BatchGetItemOutput
	DescribeTimeToLiveOut *dynamodb.DescribeTimeToLiveOutput
}

func (m *mockDynamoDB) DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return m.DescribeTableOut, nil
}

func (m *mockDynamoDB) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.GetItemOut != nil {
		return m.GetItemOut, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamoDB) BatchGetItem(ctx context.Context, params *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	if m.BatchGetItemOut != nil {
		return m.BatchGetItemOut, nil
	}
	return &dynamodb.BatchGetItemOutput{}, nil
}

func (m *mockDynamoDB) DescribeTimeToLive(ctx context.Context, params *dynamodb.DescribeTimeToLiveInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTimeToLiveOutput, error) {
	if m.DescribeTimeToLiveOut != nil {
		return m.DescribeTimeToLiveOut, nil
	}
	return &dynamodb.DescribeTimeToLiveOutput{}, nil
}

// NewRiskAnalyzerMock creates a RiskAnalyzer suitable for unit tests.
func NewRiskAnalyzerMock(mock *mockDynamoDB) *RiskAnalyzer {
	cfg := &engine.AppConfig{}
	guard := engine.NewGuardrail(cfg)
	return &RiskAnalyzer{
		config:    cfg,
		db:        mock,
		guardrail: guard,
	}
}
