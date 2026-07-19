package dynamo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/International-Combat-Archery-Alliance/voting-api/slices"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

type voteRecordDynamo struct {
	PK             string
	SK             string
	PollID         string
	IdempotencyKey string
	OptionIDs      []string
	CreatedAt      time.Time
	TTL            int64
}

type resultsDynamo struct {
	PK         string
	SK         string
	TotalVotes int
	Counts     map[string]int
}

func voteRecordSK(idempotencyKey string) string {
	return fmt.Sprintf("IDEMPOTENCY#%s", idempotencyKey)
}

func newVoteRecordDynamo(record polls.VoteRecord) voteRecordDynamo {
	return voteRecordDynamo{
		PK:             pollPK(record.PollID),
		SK:             voteRecordSK(record.IdempotencyKey),
		PollID:         record.PollID.String(),
		IdempotencyKey: record.IdempotencyKey,
		OptionIDs: slices.Map(record.OptionIDs, func(id uuid.UUID) string {
			return id.String()
		}),
		CreatedAt: record.CreatedAt.UTC(),
		TTL:       record.TTL.Unix(),
	}
}

func voteRecordFromDynamo(record voteRecordDynamo) polls.VoteRecord {
	return polls.VoteRecord{
		PollID:         uuid.MustParse(record.PollID),
		IdempotencyKey: record.IdempotencyKey,
		OptionIDs: slices.Map(record.OptionIDs, func(id string) uuid.UUID {
			return uuid.MustParse(id)
		}),
		CreatedAt: record.CreatedAt.UTC(),
		TTL:       time.Unix(record.TTL, 0).UTC(),
	}
}

func (d *DB) GetVoteRecord(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	resp, err := d.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pollPK(pollID)},
			"SK": &types.AttributeValueMemberS{Value: voteRecordSK(idempotencyKey)},
		},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return polls.VoteRecord{}, polls.NewTimeoutError("GetVoteRecord timed out")
		}
		return polls.VoteRecord{}, polls.NewFailedToFetchError("Failed to fetch vote record from dynamo", err)
	}

	if len(resp.Item) == 0 {
		return polls.VoteRecord{}, polls.NewVoteRecordNotFoundError(fmt.Sprintf("No vote record with key %q", idempotencyKey))
	}

	var record voteRecordDynamo
	err = attributevalue.UnmarshalMap(resp.Item, &record)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal vote record from DB: %s", err))
	}
	return voteRecordFromDynamo(record), nil
}

func (d *DB) RecordVote(ctx context.Context, record polls.VoteRecord) error {
	ctx, cancel := context.WithTimeoutCause(ctx, time.Second, polls.NewTimeoutError("RecordVote to DB took too long"))
	defer cancel()

	recordItem, err := attributevalue.MarshalMap(newVoteRecordDynamo(record))
	if err != nil {
		return polls.NewFailedToTranslateToDBModelError("Failed to convert VoteRecord to voteRecordDynamo", err)
	}

	_, err = d.dynamoClient.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Put: &types.Put{
					TableName:           aws.String(d.tableName),
					Item:                recordItem,
					ConditionExpression: aws.String("attribute_not_exists(PK)"),
				},
			},
			{
				Update: &types.Update{
					TableName: aws.String(d.tableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: pollPK(record.PollID)},
						"SK": &types.AttributeValueMemberS{Value: resultsSK},
					},
					UpdateExpression:          aws.String(incrementResultsExpression(record.OptionIDs)),
					ExpressionAttributeNames:  incrementResultsNames(record.OptionIDs),
					ExpressionAttributeValues: incrementResultsValues(),
				},
			},
		},
	})
	if err != nil {
		var txnCanceledErr *types.TransactionCanceledException
		if errors.As(err, &txnCanceledErr) && transactionFailedDueToCondition(txnCanceledErr) {
			return polls.NewVoteAlreadyRecordedError(fmt.Sprintf("A vote with key %q was already recorded", record.IdempotencyKey), err)
		} else if errors.Is(err, context.DeadlineExceeded) {
			return polls.NewTimeoutError("RecordVote timed out")
		}
		return polls.NewFailedToWriteError("Failed TransactWriteItems call", err)
	}

	return nil
}

// incrementResultsExpression builds an update expression that increments the
// total ballot count and the count of each voted option in one atomic update
func incrementResultsExpression(optionIDs []uuid.UUID) string {
	setClauses := make([]string, 0, len(optionIDs)+1)
	setClauses = append(setClauses, "#total = if_not_exists(#total, :zero) + :one")
	for i := range optionIDs {
		setClauses = append(setClauses, fmt.Sprintf("#counts.#k%d = if_not_exists(#counts.#k%d, :zero) + :one", i, i))
	}

	return fmt.Sprintf("SET %s", strings.Join(setClauses, ", "))
}

func incrementResultsNames(optionIDs []uuid.UUID) map[string]string {
	names := map[string]string{
		"#total":  "TotalVotes",
		"#counts": "Counts",
	}
	for i, id := range optionIDs {
		names[fmt.Sprintf("#k%d", i)] = id.String()
	}
	return names
}

func incrementResultsValues() map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		":zero": &types.AttributeValueMemberN{Value: "0"},
		":one":  &types.AttributeValueMemberN{Value: "1"},
	}
}

func (d *DB) GetResults(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	resp, err := d.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pollPK(pollID)},
			"SK": &types.AttributeValueMemberS{Value: resultsSK},
		},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return polls.Results{}, polls.NewTimeoutError("GetResults timed out")
		}
		return polls.Results{}, polls.NewFailedToFetchError("Failed to fetch results from dynamo", err)
	}

	// No votes have been cast yet
	if len(resp.Item) == 0 {
		return polls.Results{
			PollID: pollID,
			Counts: map[uuid.UUID]int{},
		}, nil
	}

	var results resultsDynamo
	err = attributevalue.UnmarshalMap(resp.Item, &results)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal results from DB: %s", err))
	}

	counts := make(map[uuid.UUID]int, len(results.Counts))
	for k, v := range results.Counts {
		counts[uuid.MustParse(k)] = v
	}

	return polls.Results{
		PollID:     pollID,
		TotalVotes: results.TotalVotes,
		Counts:     counts,
	}, nil
}
