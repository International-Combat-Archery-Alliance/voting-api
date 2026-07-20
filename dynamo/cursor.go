package dynamo

import (
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func lastEvalKeyToCursor(lastEvalKey map[string]types.AttributeValue) (string, error) {
	bytesJSON, err := attributevalue.MarshalMapJSON(lastEvalKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode to JSON: %w", err)
	}

	return base64.StdEncoding.EncodeToString(bytesJSON), nil
}

func cursorToLastEval(cursor string) (map[string]types.AttributeValue, error) {
	bytesJSON, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to b64 decode: %w", err)
	}

	outputJSON, err := attributevalue.UnmarshalMapJSON(bytesJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to json decode: %w", err)
	}

	return outputJSON, nil
}

// gsi1KeyAttrs are the attributes that make up a LastEvaluatedKey for queries
// on GSI1: the table's primary key plus the index's key. DynamoDB requires
// all of them in an ExclusiveStartKey for index queries.
var gsi1KeyAttrs = []string{"PK", "SK", "GSI1PK", "GSI1SK"}

// nextCursor returns the cursor of where the next page of a query should
// resume from, or nil if there is no next page.
//
// Queries fetch limit+1 items so that the presence of an extra item signals
// another page. When the extra item is present, the cursor must point at the
// last item actually given to the user so the extra item is refetched on the
// next page. LastEvaluatedKey cannot be used in that case: it points past the
// extra item, and is not guaranteed to be present at all. Otherwise, a
// non-empty LastEvaluatedKey means DynamoDB stopped before reaching the limit
// (e.g. the 1MB page limit). Note DynamoDB may return a LastEvaluatedKey even
// when no items remain, in which case the next page will simply be empty.
func nextCursor(items []map[string]types.AttributeValue, limit int32, lastEvalKey map[string]types.AttributeValue) (*string, error) {
	var key map[string]types.AttributeValue
	switch {
	case len(items) > int(limit):
		key = getKeyFromItem(items[limit-1], gsi1KeyAttrs)
	case len(lastEvalKey) > 0:
		key = lastEvalKey
	default:
		return nil, nil
	}

	cursor, err := lastEvalKeyToCursor(key)
	if err != nil {
		return nil, err
	}
	return &cursor, nil
}

func getKeyFromItem(item map[string]types.AttributeValue, keyAttrs []string) map[string]types.AttributeValue {
	result := map[string]types.AttributeValue{}
	for _, attr := range keyAttrs {
		result[attr] = item[attr]
	}
	return result
}
