package dynamo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/International-Combat-Archery-Alliance/voting-api/slices"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

var _ polls.Repository = &DB{}

type pollDynamo struct {
	PK                    string
	SK                    string
	GSI1PK                string
	GSI1SK                string
	ID                    string
	Version               int
	Name                  string
	Description           *string
	StartTime             time.Time
	EndTime               time.Time
	ResultsVisibility     polls.ResultsVisibility
	PublicResultsLevel    polls.PublicResultsLevel
	MaxSelections         int
	MaxSelectionsPerGroup *int
	Groups                []groupDynamo
	Options               []optionDynamo
}

type groupDynamo struct {
	ID       string
	Name     string
	Color    *string
	ImageURL *string
}

type optionDynamo struct {
	ID       string
	GroupID  *string
	Name     string
	Subtitle *string
	ImageURL *string
}

const (
	pollEntityName = "POLL"
	pollMetaSK     = "#METADATA"
	resultsSK      = "RESULTS"
)

func pollPK(id uuid.UUID) string {
	return fmt.Sprintf("%s#%s", pollEntityName, id)
}

func newPollDynamo(poll polls.Poll) pollDynamo {
	return pollDynamo{
		PK:     pollPK(poll.ID),
		SK:     pollMetaSK,
		GSI1PK: pollEntityName,
		GSI1SK: fmt.Sprintf("%s#%s#%s", pollEntityName, poll.StartTime.UTC().Format(time.RFC3339), poll.ID),
		ID:     poll.ID.String(),
		// Store timestamps in the db as UTC
		StartTime:             poll.StartTime.UTC(),
		EndTime:               poll.EndTime.UTC(),
		Version:               poll.Version,
		Name:                  poll.Name,
		Description:           poll.Description,
		ResultsVisibility:     poll.ResultsVisibility,
		PublicResultsLevel:    poll.PublicResultsLevel,
		MaxSelections:         poll.VoteConfig.MaxSelections,
		MaxSelectionsPerGroup: poll.VoteConfig.MaxSelectionsPerGroup,
		Groups: slices.Map(poll.Groups, func(g polls.Group) groupDynamo {
			return groupDynamo{
				ID:       g.ID.String(),
				Name:     g.Name,
				Color:    g.Color,
				ImageURL: g.ImageURL,
			}
		}),
		Options: slices.Map(poll.Options, func(o polls.Option) optionDynamo {
			var groupID *string
			if o.GroupID != nil {
				s := o.GroupID.String()
				groupID = &s
			}
			return optionDynamo{
				ID:       o.ID.String(),
				GroupID:  groupID,
				Name:     o.Name,
				Subtitle: o.Subtitle,
				ImageURL: o.ImageURL,
			}
		}),
	}
}

func pollFromPollDynamo(poll pollDynamo) polls.Poll {
	return polls.Poll{
		ID:          uuid.MustParse(poll.ID),
		Version:     poll.Version,
		Name:        poll.Name,
		Description: poll.Description,
		StartTime:   poll.StartTime.UTC(),
		EndTime:     poll.EndTime.UTC(),
		VoteConfig: polls.VoteConfig{
			MaxSelections:         poll.MaxSelections,
			MaxSelectionsPerGroup: poll.MaxSelectionsPerGroup,
		},
		ResultsVisibility:  poll.ResultsVisibility,
		PublicResultsLevel: poll.PublicResultsLevel,
		Groups: slices.Map(poll.Groups, func(g groupDynamo) polls.Group {
			return polls.Group{
				ID:       uuid.MustParse(g.ID),
				Name:     g.Name,
				Color:    g.Color,
				ImageURL: g.ImageURL,
			}
		}),
		Options: slices.Map(poll.Options, func(o optionDynamo) polls.Option {
			var groupID *uuid.UUID
			if o.GroupID != nil {
				id := uuid.MustParse(*o.GroupID)
				groupID = &id
			}
			return polls.Option{
				ID:       uuid.MustParse(o.ID),
				GroupID:  groupID,
				Name:     o.Name,
				Subtitle: o.Subtitle,
				ImageURL: o.ImageURL,
			}
		}),
	}
}

func (d *DB) CreatePoll(ctx context.Context, poll polls.Poll) error {
	ctx, cancel := context.WithTimeoutCause(ctx, time.Second, polls.NewTimeoutError("CreatePoll to DB took too long"))
	defer cancel()

	dynamoItem := newPollDynamo(poll)

	item, err := attributevalue.MarshalMap(dynamoItem)
	if err != nil {
		return polls.NewFailedToTranslateToDBModelError("Failed to convert Poll to pollDynamo", err)
	}

	resultsItem, err := attributevalue.MarshalMap(resultsDynamo{
		PK:         pollPK(poll.ID),
		SK:         resultsSK,
		TotalVotes: 0,
		Counts:     map[string]int{},
	})
	if err != nil {
		return polls.NewFailedToTranslateToDBModelError("Failed to convert Results to resultsDynamo", err)
	}

	expr := exprMustBuild(expression.NewBuilder().
		WithCondition(newEntityVersionConditional(dynamoItem.Version)))

	// The results item is created alongside the poll so that vote updates can
	// increment nested count paths, which requires the parent map to exist
	_, err = d.dynamoClient.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Put: &types.Put{
					TableName:                 aws.String(d.tableName),
					Item:                      item,
					ConditionExpression:       expr.Condition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				},
			},
			{
				Put: &types.Put{
					TableName: aws.String(d.tableName),
					Item:      resultsItem,
				},
			},
		},
	})
	if err != nil {
		var txnCanceledErr *types.TransactionCanceledException
		if errors.As(err, &txnCanceledErr) && transactionFailedDueToCondition(txnCanceledErr) {
			return polls.NewPollAlreadyExistsError(fmt.Sprintf("Poll with ID %q already exists", poll.ID), err)
		} else if errors.Is(err, context.DeadlineExceeded) {
			return polls.NewTimeoutError("CreatePoll timed out")
		} else {
			return polls.NewFailedToWriteError("Failed TransactWriteItems call", err)
		}
	}

	return nil
}

func (d *DB) GetPoll(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	resp, err := d.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pollPK(id)},
			"SK": &types.AttributeValueMemberS{Value: pollMetaSK},
		},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return polls.Poll{}, polls.NewTimeoutError("GetPoll timed out")
		}
		return polls.Poll{}, polls.NewFailedToFetchError(fmt.Sprintf("Failed to fetch poll with ID %q", id), err)
	}

	if len(resp.Item) == 0 {
		return polls.Poll{}, polls.NewPollDoesNotExistError(fmt.Sprintf("Poll with ID %q not found", id), nil)
	}

	var poll pollDynamo
	err = attributevalue.UnmarshalMap(resp.Item, &poll)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal poll from DB: %s", err))
	}
	return pollFromPollDynamo(poll), nil
}

func (d *DB) GetPolls(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	keyCond := expression.Key("GSI1PK").Equal(expression.Value(pollEntityName)).
		And(expression.Key("GSI1SK").BeginsWith(pollEntityName))

	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build dynamo key expression: %s", err))
	}

	var startKey map[string]types.AttributeValue
	if cursor != nil {
		startKey, err = cursorToLastEval(*cursor)
		if err != nil {
			return polls.GetPollsResponse{}, polls.NewInvalidCursorError("Invalid cursor", err)
		}
	}

	result, err := d.dynamoClient.Query(ctx, &dynamodb.QueryInput{
		IndexName:                 aws.String(gsi1),
		TableName:                 aws.String(d.tableName),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		// Want to sort newest start time first
		ScanIndexForward: aws.Bool(false),
		// Fetch 1 more than limit to check if there is another page or not
		Limit:             aws.Int32(limit + 1),
		ExclusiveStartKey: startKey,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return polls.GetPollsResponse{}, polls.NewTimeoutError("GetPolls timed out")
		}
		return polls.GetPollsResponse{}, polls.NewFailedToFetchError("Failed to fetch polls from dynamo", err)
	}

	var dynamoItems []pollDynamo
	err = attributevalue.UnmarshalListOfMaps(result.Items, &dynamoItems)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal dynamo polls: %s", err))
	}

	hasNextPage := len(dynamoItems) > int(limit) && len(result.LastEvaluatedKey) > 0

	var newCursor *string
	if hasNextPage {
		// Can't use LastEvalKey directly because we grabbed an extra item to check for next page
		lastItemGivenToUser := result.Items[len(result.Items)-2]
		lastItemKey := getKeyFromItem(result.LastEvaluatedKey, lastItemGivenToUser)
		c, err := lastEvalKeyToCursor(lastItemKey)
		if err != nil {
			panic(fmt.Sprintf("failed to make cursor from lastEvalKey: %s", err))
		}
		newCursor = &c
	}

	return polls.GetPollsResponse{
		Data: slices.Map(dynamoItems, func(v pollDynamo) polls.Poll {
			return pollFromPollDynamo(v)
		})[:min(int(limit), len(dynamoItems))],
		Cursor:      newCursor,
		HasNextPage: hasNextPage,
	}, nil
}

func (d *DB) UpdatePoll(ctx context.Context, poll polls.Poll) error {
	ctx, cancel := context.WithTimeoutCause(ctx, time.Second, polls.NewTimeoutError("UpdatePoll to DB took too long"))
	defer cancel()

	dynamoItem := newPollDynamo(poll)

	item, err := attributevalue.MarshalMap(dynamoItem)
	if err != nil {
		return polls.NewFailedToTranslateToDBModelError("Failed to convert Poll to pollDynamo", err)
	}

	expr := exprMustBuild(expression.NewBuilder().
		WithCondition(existingEntityVersionConditional(dynamoItem.Version)))

	_, err = d.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 aws.String(d.tableName),
		Item:                      item,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		var condCheckFailedErr *types.ConditionalCheckFailedException
		if errors.As(err, &condCheckFailedErr) {
			return polls.NewVersionConflictError(fmt.Sprintf("Poll with ID %q was modified by another request", poll.ID), err)
		} else if errors.Is(err, context.DeadlineExceeded) {
			return polls.NewTimeoutError("UpdatePoll timed out")
		} else {
			return polls.NewFailedToWriteError("Failed PutItem call", err)
		}
	}

	return nil
}

func (d *DB) DeletePoll(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeoutCause(ctx, time.Second, polls.NewTimeoutError("DeletePoll to DB took too long"))
	defer cancel()

	existsExpr := exprMustBuild(expression.NewBuilder().
		WithCondition(expression.Name("PK").AttributeExists()))

	keyCond := expression.Key("PK").Equal(expression.Value(pollPK(id))).
		And(expression.Key("SK").BeginsWith("IDEMPOTENCY#"))

	keyExpr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build dynamo key expression: %s", err))
	}

	idempotencyResult, err := d.dynamoClient.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(d.tableName),
		KeyConditionExpression:    keyExpr.KeyCondition(),
		ExpressionAttributeNames:  keyExpr.Names(),
		ExpressionAttributeValues: keyExpr.Values(),
	})
	if err != nil {
		return polls.NewFailedToFetchError("Failed to query idempotency records for deletion", err)
	}

	transactItems := []types.TransactWriteItem{
		{
			Delete: &types.Delete{
				TableName: aws.String(d.tableName),
				Key: map[string]types.AttributeValue{
					"PK": &types.AttributeValueMemberS{Value: pollPK(id)},
					"SK": &types.AttributeValueMemberS{Value: pollMetaSK},
				},
				ConditionExpression:       existsExpr.Condition(),
				ExpressionAttributeNames:  existsExpr.Names(),
				ExpressionAttributeValues: existsExpr.Values(),
			},
		},
		{
			Delete: &types.Delete{
				TableName: aws.String(d.tableName),
				Key: map[string]types.AttributeValue{
					"PK": &types.AttributeValueMemberS{Value: pollPK(id)},
					"SK": &types.AttributeValueMemberS{Value: resultsSK},
				},
			},
		},
	}

	for _, item := range idempotencyResult.Items {
		transactItems = append(transactItems, types.TransactWriteItem{
			Delete: &types.Delete{
				TableName: aws.String(d.tableName),
				Key: map[string]types.AttributeValue{
					"PK": item["PK"],
					"SK": item["SK"],
				},
			},
		})
	}

	_, err = d.dynamoClient.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	})
	if err != nil {
		var txnCanceledErr *types.TransactionCanceledException
		if errors.As(err, &txnCanceledErr) && transactionFailedDueToCondition(txnCanceledErr) {
			return polls.NewPollDoesNotExistError(fmt.Sprintf("Poll with ID %q not found", id), err)
		} else if errors.Is(err, context.DeadlineExceeded) {
			return polls.NewTimeoutError("DeletePoll timed out")
		}
		return polls.NewFailedToWriteError("Failed TransactWriteItems call", err)
	}

	return nil
}

// transactionFailedDueToCondition returns true if any of the transaction's
// cancellation reasons was a failed condition check
func transactionFailedDueToCondition(err *types.TransactionCanceledException) bool {
	for _, reason := range err.CancellationReasons {
		if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
			return true
		}
	}
	return false
}
