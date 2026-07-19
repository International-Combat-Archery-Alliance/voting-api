package polls

import (
	"testing"

	"github.com/International-Combat-Archery-Alliance/voting-api/ptr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestValidateBallot(t *testing.T) {
	activeTime := newTestPoll().StartTime

	t.Run("valid single vote", func(t *testing.T) {
		poll := newTestPoll()

		assert.NoError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID}, activeTime))
	})

	t.Run("rejects vote before start time", func(t *testing.T) {
		poll := newTestPoll()

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID}, poll.StartTime.Add(-1)), REASON_POLL_NOT_ACTIVE)
	})

	t.Run("rejects vote after end time", func(t *testing.T) {
		poll := newTestPoll()

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID}, poll.EndTime.Add(1)), REASON_POLL_NOT_ACTIVE)
	})

	t.Run("rejects empty ballot", func(t *testing.T) {
		poll := newTestPoll()

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{}, activeTime), REASON_INVALID_BALLOT)
	})

	t.Run("rejects more than max selections", func(t *testing.T) {
		poll := newTestPoll()

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID, poll.Options[1].ID}, activeTime), REASON_INVALID_BALLOT)
	})

	t.Run("allows multiple selections when configured", func(t *testing.T) {
		poll := newTestPoll()
		poll.VoteConfig.MaxSelections = 2

		assert.NoError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID, poll.Options[1].ID}, activeTime))
	})

	t.Run("rejects duplicate options", func(t *testing.T) {
		poll := newTestPoll()
		poll.VoteConfig.MaxSelections = 2

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID, poll.Options[0].ID}, activeTime), REASON_INVALID_BALLOT)
	})

	t.Run("rejects unknown options", func(t *testing.T) {
		poll := newTestPoll()

		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{uuid.New()}, activeTime), REASON_INVALID_BALLOT)
	})

	t.Run("rejects more than max selections per group", func(t *testing.T) {
		poll := newTestPoll()
		groupID := *poll.Options[0].GroupID
		poll.Options = append(poll.Options, Option{ID: uuid.New(), GroupID: &groupID, Name: "Teammate"})
		poll.VoteConfig.MaxSelections = 2
		poll.VoteConfig.MaxSelectionsPerGroup = ptr.Int(1)

		// Options 0 and 2 are in the same group
		assertBallotError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID, poll.Options[2].ID}, activeTime), REASON_INVALID_BALLOT)
	})

	t.Run("allows selections across groups within per group limit", func(t *testing.T) {
		poll := newTestPoll()
		groupID := *poll.Options[0].GroupID
		poll.Options[1].GroupID = nil
		poll.Options = append(poll.Options, Option{ID: uuid.New(), GroupID: &groupID, Name: "Teammate"})
		poll.VoteConfig.MaxSelections = 2
		poll.VoteConfig.MaxSelectionsPerGroup = ptr.Int(1)

		assert.NoError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[0].ID, poll.Options[1].ID}, activeTime))
	})

	t.Run("ungrouped options are not subject to per group limit", func(t *testing.T) {
		poll := newTestPoll()
		poll.Options = append(poll.Options, Option{ID: uuid.New(), Name: "Ungrouped"})
		poll.VoteConfig.MaxSelections = 2
		poll.VoteConfig.MaxSelectionsPerGroup = ptr.Int(1)

		assert.NoError(t, poll.ValidateBallot([]uuid.UUID{poll.Options[1].ID, poll.Options[2].ID}, activeTime))
	})
}

func assertBallotError(t *testing.T, err error, reason ErrorReason) {
	t.Helper()

	var pollsErr *Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, reason, pollsErr.Reason)
}
