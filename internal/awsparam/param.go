// Package awsparam provides AWS Systems Manager Parameter Store utilities.
package awsparam

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)


func GetSSMParam(ctx context.Context, name string) (string, error) {
	// Deprecated endpoint resolver removed. We'll use BaseEndpoint option when creating the SSM client.
	// Auto-detect AWS region from environment or config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	var sstmClient *ssm.Client
	if endpoint := os.Getenv("AWS_BASE_ENDPOINT"); endpoint != "" {
		sstmClient = ssm.NewFromConfig(cfg, func(o *ssm.Options) {
			o.BaseEndpoint = &endpoint
		})
	} else {
		sstmClient = ssm.NewFromConfig(cfg)
	}

	input := &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	}

	out, err := sstmClient.GetParameter(ctx, input)
	if err != nil {
		return "", fmt.Errorf("getting parameter %s: %w", name, err)
	}

	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("no value found for parameter %s", name)
	}

	return *out.Parameter.Value, nil
}