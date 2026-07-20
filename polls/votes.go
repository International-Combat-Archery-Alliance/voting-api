package polls

import (
	"context"
	"fmt"
	"math"
	"sort"
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

// Percentages returns the percentage of votes each option received, using the
// largest remainder method to ensure the results always sum to 100.
// Options with zero total votes all get 0%.
func (r Results) Percentages() map[uuid.UUID]int {
	pcts := make(map[uuid.UUID]int, len(r.Counts))
	if r.TotalVotes == 0 {
		for id := range r.Counts {
			pcts[id] = 0
		}
		return pcts
	}

	type entry struct {
		id        uuid.UUID
		exact     float64
		remainder float64
		allocated int
	}
	entries := make([]entry, 0, len(r.Counts))
	allocatedTotal := 0
	for id, count := range r.Counts {
		exact := float64(count) / float64(r.TotalVotes) * 100.0
		floored := int(math.Floor(exact))
		entries = append(entries, entry{
			id:        id,
			exact:     exact,
			remainder: exact - float64(floored),
			allocated: floored,
		})
		allocatedTotal += floored
	}

	remaining := 100 - allocatedTotal
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].remainder != entries[j].remainder {
			return entries[i].remainder > entries[j].remainder
		}
		return entries[i].exact > entries[j].exact
	})

	for i := 0; i < remaining && i < len(entries); i++ {
		entries[i].allocated++
	}

	for _, e := range entries {
		pcts[e.id] = e.allocated
	}
	return pcts
}

// Filtered returns a copy of the results containing only counts for the given options,
// with TotalVotes updated to the sum of those counts.
func (r Results) Filtered(options []Option) Results {
	filtered := Results{
		PollID: r.PollID,
		Counts: make(map[uuid.UUID]int, len(options)),
	}
	for _, o := range options {
		count := r.Counts[o.ID]
		filtered.Counts[o.ID] = count
		filtered.TotalVotes += count
	}
	return filtered
}

// Rankings returns the 1-based rank of each option, with ties receiving the same rank
// and the next rank skipping (e.g. 1, 2, 2, 4). Options are sorted by count descending.
func (r Results) Rankings() map[uuid.UUID]int {
	type entry struct {
		id    uuid.UUID
		count int
	}
	entries := make([]entry, 0, len(r.Counts))
	for id, count := range r.Counts {
		entries = append(entries, entry{id: id, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	ranks := make(map[uuid.UUID]int, len(entries))
	for i, e := range entries {
		if i > 0 && e.count == entries[i-1].count {
			ranks[e.id] = ranks[entries[i-1].id]
		} else {
			ranks[e.id] = i + 1
		}
	}
	return ranks
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
