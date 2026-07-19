package polls

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// VoteRecord is the record of a cast ballot, keyed by the client generated
// idempotency key to protect against double counting retried requests
type VoteRecord struct {
	PollID         uuid.UUID
	IdempotencyKey string
	OptionIDs      []uuid.UUID
	CreatedAt      time.Time
	// TTL is when the record expires and is cleaned up from the DB
	TTL time.Time
}

type Results struct {
	PollID uuid.UUID
	// TotalVotes is the total amount of ballots cast
	TotalVotes int
	// Counts is the amount of votes per option ID
	Counts map[uuid.UUID]int
}

type Repository interface {
	CreatePoll(ctx context.Context, poll Poll) error
	GetPoll(ctx context.Context, id uuid.UUID) (Poll, error)
	GetPolls(ctx context.Context, limit int32, cursor *string) (GetPollsResponse, error)
	UpdatePoll(ctx context.Context, poll Poll) error
	DeletePoll(ctx context.Context, id uuid.UUID) error
	GetResults(ctx context.Context, pollID uuid.UUID) (Results, error)
	GetVoteRecord(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (VoteRecord, error)
	RecordVote(ctx context.Context, record VoteRecord) error
}

// ValidateBallot validates a ballot against the poll's status and vote config at the given time
func (p Poll) ValidateBallot(optionIDs []uuid.UUID, now time.Time) error {
	if !p.IsActive(now) {
		return NewPollNotActiveError(fmt.Sprintf("Poll with ID %q is not currently active", p.ID))
	}

	if len(optionIDs) == 0 {
		return NewInvalidBallotError("ballot must contain at least one option")
	}

	if len(optionIDs) > p.VoteConfig.MaxSelections {
		return NewInvalidBallotError(fmt.Sprintf("ballot contains more than the max of %d selections", p.VoteConfig.MaxSelections))
	}

	optionsByID := make(map[uuid.UUID]Option, len(p.Options))
	for _, o := range p.Options {
		optionsByID[o.ID] = o
	}

	seen := make(map[uuid.UUID]struct{}, len(optionIDs))
	groupCounts := map[uuid.UUID]int{}
	for _, id := range optionIDs {
		if _, ok := seen[id]; ok {
			return NewInvalidBallotError("ballot contains a duplicate option")
		}
		seen[id] = struct{}{}

		option, ok := optionsByID[id]
		if !ok {
			return NewInvalidBallotError(fmt.Sprintf("option with ID %q does not exist in this poll", id))
		}

		if p.VoteConfig.MaxSelectionsPerGroup != nil && option.GroupID != nil {
			groupCounts[*option.GroupID]++
			if groupCounts[*option.GroupID] > *p.VoteConfig.MaxSelectionsPerGroup {
				return NewInvalidBallotError(fmt.Sprintf("ballot contains more than the max of %d selections per group", *p.VoteConfig.MaxSelectionsPerGroup))
			}
		}
	}

	return nil
}
