package dynamo

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

const (
	gsi1 = "GSI1"
)

type DB struct {
	dynamoClient *dynamodb.Client
	tableName    string
}

func NewDB(dynamoClient *dynamodb.Client, tableName string) *DB {
	return &DB{
		dynamoClient: dynamoClient,
		tableName:    tableName,
	}
}

func newEntityVersionConditional(version int) expression.ConditionBuilder {
	return expression.Name("PK").AttributeNotExists().
		And(expression.Value(version).Equal(expression.Value(1)))
}

func existingEntityVersionConditional(version int) expression.ConditionBuilder {
	return expression.Name("PK").AttributeExists().
		And(expression.Name("Version").Equal(expression.Value(version - 1)))
}

func exprMustBuild(builder expression.Builder) expression.Expression {
	expr, err := builder.Build()
	if err != nil {
		panic("failed to build dynamo expression")
	}

	return expr
}
