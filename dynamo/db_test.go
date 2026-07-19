package dynamo

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	container "github.com/testcontainers/testcontainers-go/modules/dynamodb"
)

var dynamodbTestContainer *container.DynamoDBContainer
var dynamoClient *dynamodb.Client
var db *DB

const tableName = "Voting-Test"

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()

	err := setupDynamo(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer shutdownDynamo(ctx)

	os.Exit(m.Run())
}

func setupDynamo(ctx context.Context) error {
	if _, ok := os.LookupEnv("TEST_IN_CI"); ok {
		return setupDynamoInCI(ctx)
	}

	return setupDynamoTestContainers(ctx)
}

func setupDynamoTestContainers(ctx context.Context) error {
	var err error
	dynamodbTestContainer, err = container.Run(ctx, "amazon/dynamodb-local")
	if err != nil {
		return fmt.Errorf("error starting dynamo testcontainer: %w", err)
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("localhost"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy")),
	)
	if err != nil {
		return fmt.Errorf("error making dynamo config: %w", err)
	}

	endpoint, err := dynamodbTestContainer.Endpoint(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to get endpoint: %w", err)
	}

	dynamoClient = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("http://%s", endpoint))
	})

	err = makeTable(ctx)
	if err != nil {
		return err
	}

	db = NewDB(dynamoClient, tableName)

	return nil
}

func setupDynamoInCI(ctx context.Context) error {
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
		return fmt.Errorf("aws config error: %w", err)
	}

	dynamoClient = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String("http://localhost:8000")
	})

	err = makeTable(ctx)
	if err != nil {
		return err
	}

	db = NewDB(dynamoClient, tableName)

	return nil
}

func makeTable(ctx context.Context) error {
	_, err := dynamoClient.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(tableName),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("PK"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("SK"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("GSI1PK"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("GSI1SK"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("PK"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("SK"),
				KeyType:       types.KeyTypeRange,
			},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String(gsi1),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("GSI1PK"),
						KeyType:       types.KeyTypeHash,
					},
					{
						AttributeName: aws.String("GSI1SK"),
						KeyType:       types.KeyTypeRange,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

func resetTable(ctx context.Context) {
	_, err := dynamoClient.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		fmt.Printf("failed to delete table: %s", err)
	}

	err = makeTable(ctx)
	if err != nil {
		fmt.Printf("failed to remake table: %s", err)
	}
}

func shutdownDynamo(ctx context.Context) {
	if dynamodbTestContainer == nil {
		return
	}

	err := dynamodbTestContainer.Terminate(ctx)
	if err != nil {
		fmt.Printf("error terminating dynamo testcontainer: %s\n", err)
	}
}
