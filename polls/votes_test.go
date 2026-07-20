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

func TestPercentages(t *testing.T) {
	t.Run("percentages always sum to 100", func(t *testing.T) {
		a, b, c := uuid.New(), uuid.New(), uuid.New()
		results := Results{
			TotalVotes: 7,
			Counts: map[uuid.UUID]int{
				a: 3,
				b: 2,
				c: 2,
			},
		}
		pcts := results.Percentages()
		assert.Equal(t, 3, len(pcts))
		sum := 0
		for _, p := range pcts {
			sum += p
		}
		assert.Equal(t, 100, sum)
	})

	t.Run("clean 50/50 split", func(t *testing.T) {
		a, b := uuid.New(), uuid.New()
		results := Results{
			TotalVotes: 10,
			Counts: map[uuid.UUID]int{
				a: 5,
				b: 5,
			},
		}
		pcts := results.Percentages()
		assert.Equal(t, 50, pcts[a])
		assert.Equal(t, 50, pcts[b])
	})

	t.Run("zero total votes gives all zeros", func(t *testing.T) {
		a, b := uuid.New(), uuid.New()
		results := Results{
			TotalVotes: 0,
			Counts: map[uuid.UUID]int{
				a: 0,
				b: 0,
			},
		}
		pcts := results.Percentages()
		assert.Equal(t, 0, pcts[a])
		assert.Equal(t, 0, pcts[b])
	})
}

func TestRankings(t *testing.T) {
	t.Run("computes rankings with ties", func(t *testing.T) {
		a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		results := Results{
			Counts: map[uuid.UUID]int{
				a: 10,
				b: 10,
				c: 5,
				d: 3,
			},
		}
		ranks := results.Rankings()
		assert.Equal(t, 1, ranks[a])
		assert.Equal(t, 1, ranks[b])
		assert.Equal(t, 3, ranks[c])
		assert.Equal(t, 4, ranks[d])
	})

	t.Run("all zero counts gives all rank 1", func(t *testing.T) {
		a, b := uuid.New(), uuid.New()
		results := Results{
			Counts: map[uuid.UUID]int{
				a: 0,
				b: 0,
			},
		}
		ranks := results.Rankings()
		assert.Equal(t, 1, ranks[a])
		assert.Equal(t, 1, ranks[b])
	})
}

func TestResultsFiltered(t *testing.T) {
	t.Run("filters to only given options", func(t *testing.T) {
		a, b, deleted := uuid.New(), uuid.New(), uuid.New()
		results := Results{
			PollID:     uuid.New(),
			TotalVotes: 10,
			Counts: map[uuid.UUID]int{
				a:       4,
				b:       3,
				deleted: 3,
			},
		}
		options := []Option{
			{ID: a, Name: "A"},
			{ID: b, Name: "B"},
		}
		filtered := results.Filtered(options)
		assert.Equal(t, 2, len(filtered.Counts))
		assert.Equal(t, 4, filtered.Counts[a])
		assert.Equal(t, 3, filtered.Counts[b])
		_, ok := filtered.Counts[deleted]
		assert.False(t, ok)
		assert.Equal(t, 7, filtered.TotalVotes)
	})

	t.Run("percentages from filtered results sum to 100", func(t *testing.T) {
		a, b, deleted := uuid.New(), uuid.New(), uuid.New()
		results := Results{
			TotalVotes: 10,
			Counts: map[uuid.UUID]int{
				a:       4,
				b:       3,
				deleted: 3,
			},
		}
		options := []Option{
			{ID: a, Name: "A"},
			{ID: b, Name: "B"},
		}
		pcts := results.Filtered(options).Percentages()
		sum := 0
		for _, p := range pcts {
			sum += p
		}
		assert.Equal(t, 100, sum)
	})
}
