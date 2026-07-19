package dynamo

import (
	"context"
	"testing"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestVoteRecord(pollID uuid.UUID, optionIDs ...uuid.UUID) polls.VoteRecord {
	return polls.VoteRecord{
		PollID:         pollID,
		IdempotencyKey: uuid.NewString(),
		OptionIDs:      optionIDs,
		CreatedAt:      time.Now(),
		TTL:            time.Now().Add(24 * time.Hour),
	}
}

func TestRecordVote(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))
	pollID := poll.ID
	option1 := uuid.New()
	option2 := uuid.New()

	require.NoError(t, db.RecordVote(context.Background(), newTestVoteRecord(pollID, option1)))
	require.NoError(t, db.RecordVote(context.Background(), newTestVoteRecord(pollID, option1, option2)))
	require.NoError(t, db.RecordVote(context.Background(), newTestVoteRecord(pollID, option2)))

	results, err := db.GetResults(context.Background(), pollID)
	require.NoError(t, err)

	assert.Equal(t, pollID, results.PollID)
	assert.Equal(t, 3, results.TotalVotes)
	assert.Equal(t, 2, results.Counts[option1])
	assert.Equal(t, 2, results.Counts[option2])
}

func TestRecordVoteWithSameKeyIsRejected(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))
	pollID := poll.ID
	option1 := uuid.New()

	record := newTestVoteRecord(pollID, option1)
	require.NoError(t, db.RecordVote(context.Background(), record))

	err := db.RecordVote(context.Background(), record)

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_VOTE_ALREADY_RECORDED, pollsErr.Reason)

	// And the counts were not double counted
	results, err := db.GetResults(context.Background(), pollID)
	require.NoError(t, err)
	assert.Equal(t, 1, results.TotalVotes)
	assert.Equal(t, 1, results.Counts[option1])
}

func TestGetVoteRecord(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))
	pollID := poll.ID
	record := newTestVoteRecord(pollID, uuid.New(), uuid.New())

	require.NoError(t, db.RecordVote(context.Background(), record))

	fetchedRecord, err := db.GetVoteRecord(context.Background(), pollID, record.IdempotencyKey)
	require.NoError(t, err)

	assert.Equal(t, record.PollID, fetchedRecord.PollID)
	assert.Equal(t, record.IdempotencyKey, fetchedRecord.IdempotencyKey)
	assert.Equal(t, record.OptionIDs, fetchedRecord.OptionIDs)
	assert.Equal(t, record.CreatedAt.UTC().Truncate(time.Microsecond), fetchedRecord.CreatedAt.Truncate(time.Microsecond))
	assert.Equal(t, record.TTL.Unix(), fetchedRecord.TTL.Unix())
}

func TestGetVoteRecordThatDoesNotExist(t *testing.T) {
	resetTable(context.Background())

	_, err := db.GetVoteRecord(context.Background(), uuid.New(), uuid.NewString())

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_VOTE_RECORD_NOT_FOUND, pollsErr.Reason)
}

func TestGetResultsWithNoVotes(t *testing.T) {
	resetTable(context.Background())

	pollID := uuid.New()

	results, err := db.GetResults(context.Background(), pollID)
	require.NoError(t, err)

	assert.Equal(t, pollID, results.PollID)
	assert.Equal(t, 0, results.TotalVotes)
	assert.Empty(t, results.Counts)
}

func TestVoteRecordsAreScopedPerPoll(t *testing.T) {
	resetTable(context.Background())

	pollA := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), pollA))
	pollB := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), pollB))

	poll1 := pollA.ID
	poll2 := pollB.ID
	option := uuid.New()

	record := newTestVoteRecord(poll1, option)
	require.NoError(t, db.RecordVote(context.Background(), record))

	// Same idempotency key on a different poll is allowed
	otherPollRecord := record
	otherPollRecord.PollID = poll2
	require.NoError(t, db.RecordVote(context.Background(), otherPollRecord))
}
