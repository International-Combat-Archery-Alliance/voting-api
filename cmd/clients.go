package main

import (
	"context"
	"fmt"
	"os"

	"github.com/International-Combat-Archery-Alliance/voting-api/api"
	"github.com/International-Combat-Archery-Alliance/voting-api/dynamo"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func makeDB(ctx context.Context) (api.DB, error) {
	if isLocal() {
		return makeLocalDB(ctx)
	}
	return makeProdDB(ctx)
}

func makeLocalDB(ctx context.Context) (api.DB, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("localhost"),
		config.WithCredentialsProvider(credentials.StaticCredentialsProvider{
			Value: aws.Credentials{
				AccessKeyID: "local", SecretAccessKey: "local", SessionToken: "",
				Source: "Mock credentials used above for local instance",
			},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create local dynamo client: %w", err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String("http://dynamodb:8000")
	})

	return dynamo.NewDB(dynamoClient, os.Getenv("DYNAMO_TABLE_NAME")), nil
}

func makeProdDB(ctx context.Context) (api.DB, error) {
	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create production dynamo client: %w", err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg)
	return dynamo.NewDB(dynamoClient, os.Getenv("DYNAMO_TABLE_NAME")), nil
}
