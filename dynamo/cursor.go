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

func getKeyFromItem(key map[string]types.AttributeValue, item map[string]types.AttributeValue) map[string]types.AttributeValue {
	result := map[string]types.AttributeValue{}
	for k := range key {
		result[k] = item[k]
	}
	return result
}
