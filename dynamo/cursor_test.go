package dynamo

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeGSI1Item(n int) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK":     &types.AttributeValueMemberS{Value: fmt.Sprintf("POLL#%d", n)},
		"SK":     &types.AttributeValueMemberS{Value: "#METADATA"},
		"GSI1PK": &types.AttributeValueMemberS{Value: "POLL"},
		"GSI1SK": &types.AttributeValueMemberS{Value: fmt.Sprintf("POLL#2026-08-%02dT19:00:00Z#%d", n, n)},
		"Name":   &types.AttributeValueMemberS{Value: fmt.Sprintf("Poll %d", n)},
	}
}

func makeGSI1Items(n int) []map[string]types.AttributeValue {
	items := make([]map[string]types.AttributeValue, n)
	for i := range n {
		items[i] = makeGSI1Item(i)
	}
	return items
}

// decodeCursor is a test helper that asserts the cursor is valid and returns
// the key it encodes
func decodeCursor(t *testing.T, cursor *string) map[string]types.AttributeValue {
	t.Helper()

	require.NotNil(t, cursor)
	key, err := cursorToLastEval(*cursor)
	require.NoError(t, err)
	return key
}

func TestNextCursor(t *testing.T) {
	t.Run("no cursor when items are within the limit and there is no last eval key", func(t *testing.T) {
		cursor, err := nextCursor(makeGSI1Items(3), 5, nil)
		require.NoError(t, err)
		assert.Nil(t, cursor)
	})

	t.Run("cursor is the last eval key when dynamo stops before the limit", func(t *testing.T) {
		items := makeGSI1Items(3)
		lastEvalKey := getKeyFromItem(items[2], gsi1KeyAttrs)

		cursor, err := nextCursor(items, 5, lastEvalKey)
		require.NoError(t, err)

		assert.Equal(t, lastEvalKey, decodeCursor(t, cursor))
	})

	t.Run("cursor points at the last item given to the user when an extra item is fetched", func(t *testing.T) {
		items := makeGSI1Items(6)
		// DynamoDB's LastEvaluatedKey points at the extra 6th item, which the
		// user was not given
		lastEvalKey := getKeyFromItem(items[5], gsi1KeyAttrs)

		cursor, err := nextCursor(items, 5, lastEvalKey)
		require.NoError(t, err)

		assert.Equal(t, getKeyFromItem(items[4], gsi1KeyAttrs), decodeCursor(t, cursor))
	})

	t.Run("cursor points at the last item given to the user when the extra item comes with no last eval key", func(t *testing.T) {
		// Defensive case: an empty LastEvaluatedKey is the only guaranteed
		// end-of-results signal, so the extra item must not be dropped even
		// if DynamoDB ever omits the key
		items := makeGSI1Items(6)

		cursor, err := nextCursor(items, 5, nil)
		require.NoError(t, err)

		assert.Equal(t, getKeyFromItem(items[4], gsi1KeyAttrs), decodeCursor(t, cursor))
	})

	t.Run("cursor only contains key attributes", func(t *testing.T) {
		items := makeGSI1Items(6)

		cursor, err := nextCursor(items, 5, nil)
		require.NoError(t, err)

		key := decodeCursor(t, cursor)
		assert.Len(t, key, len(gsi1KeyAttrs))
		for _, attr := range gsi1KeyAttrs {
			assert.Contains(t, key, attr)
		}
	})
}
