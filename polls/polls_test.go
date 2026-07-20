package polls

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/ptr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func newTestPoll() Poll {
	groupID := uuid.New()
	return Poll{
		ID:                uuid.New(),
		Version:           1,
		Name:              "Eastern Finals MVP",
		StartTime:         time.Date(2026, 8, 7, 19, 0, 0, 0, time.UTC),
		EndTime:           time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		ResultsVisibility: RESULTS_VISIBILITY_AFTER_CLOSE,
		VoteConfig: VoteConfig{
			MaxSelections: 1,
		},
		Groups: []Group{
			{ID: groupID, Name: "Team Boston"},
		},
		Options: []Option{
			{ID: uuid.New(), GroupID: &groupID, Name: "Cameron Cardwell"},
			{ID: uuid.New(), Name: "Nate Langh"},
		},
	}
}

func TestStatus(t *testing.T) {
	poll := newTestPoll()

	t.Run("upcoming before start time", func(t *testing.T) {
		assert.Equal(t, STATUS_UPCOMING, poll.Status(poll.StartTime.Add(-time.Second)))
	})

	t.Run("active at start time", func(t *testing.T) {
		assert.Equal(t, STATUS_ACTIVE, poll.Status(poll.StartTime))
	})

	t.Run("active at end time", func(t *testing.T) {
		assert.Equal(t, STATUS_ACTIVE, poll.Status(poll.EndTime))
	})

	t.Run("closed after end time", func(t *testing.T) {
		assert.Equal(t, STATUS_CLOSED, poll.Status(poll.EndTime.Add(time.Second)))
	})
}

func TestCanViewResults(t *testing.T) {
	poll := newTestPoll()
	beforeClose := poll.EndTime.Add(-time.Hour)
	afterClose := poll.EndTime.Add(time.Hour)

	t.Run("live is viewable by anyone at any time", func(t *testing.T) {
		poll := newTestPoll()
		poll.ResultsVisibility = RESULTS_VISIBILITY_LIVE

		assert.True(t, poll.CanViewResults(false, beforeClose))
		assert.True(t, poll.CanViewResults(false, afterClose))
	})

	t.Run("after close is only viewable publicly after end time", func(t *testing.T) {
		poll := newTestPoll()
		poll.ResultsVisibility = RESULTS_VISIBILITY_AFTER_CLOSE

		assert.False(t, poll.CanViewResults(false, beforeClose))
		assert.True(t, poll.CanViewResults(false, afterClose))
		assert.True(t, poll.CanViewResults(true, beforeClose))
	})

	t.Run("admin only is never public", func(t *testing.T) {
		poll := newTestPoll()
		poll.ResultsVisibility = RESULTS_VISIBILITY_ADMIN_ONLY

		assert.False(t, poll.CanViewResults(false, beforeClose))
		assert.False(t, poll.CanViewResults(false, afterClose))
		assert.True(t, poll.CanViewResults(true, beforeClose))
	})
}

func TestValidatePoll(t *testing.T) {
	t.Run("valid poll", func(t *testing.T) {
		assert.NoError(t, ValidatePoll(newTestPoll()))
	})

	t.Run("end time must be after start time", func(t *testing.T) {
		poll := newTestPoll()
		poll.EndTime = poll.StartTime

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("must have at least one option", func(t *testing.T) {
		poll := newTestPoll()
		poll.Options = nil

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("unknown results visibility", func(t *testing.T) {
		poll := newTestPoll()
		poll.ResultsVisibility = "Bogus"

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("max selections must be at least 1", func(t *testing.T) {
		poll := newTestPoll()
		poll.VoteConfig.MaxSelections = 0

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("max selections cannot exceed amount of options", func(t *testing.T) {
		poll := newTestPoll()
		poll.VoteConfig.MaxSelections = len(poll.Options) + 1

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("max selections per group cannot exceed max selections", func(t *testing.T) {
		poll := newTestPoll()
		poll.VoteConfig.MaxSelections = 2
		poll.VoteConfig.MaxSelectionsPerGroup = ptr.Int(3)

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("option cannot reference an unknown group", func(t *testing.T) {
		poll := newTestPoll()
		unknownGroup := uuid.New()
		poll.Options[0].GroupID = &unknownGroup

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("duplicate group IDs are rejected", func(t *testing.T) {
		poll := newTestPoll()
		poll.Groups = append(poll.Groups, poll.Groups[0])

		assertInvalidPoll(t, ValidatePoll(poll))
	})

	t.Run("duplicate option IDs are rejected", func(t *testing.T) {
		poll := newTestPoll()
		poll.Options = append(poll.Options, poll.Options[0])

		assertInvalidPoll(t, ValidatePoll(poll))
	})
}

func assertInvalidPoll(t *testing.T, err error) {
	t.Helper()

	var pollsErr *Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, REASON_INVALID_POLL, pollsErr.Reason)
}

type mockRepository struct {
	GetPollFunc    func(ctx context.Context, id uuid.UUID) (Poll, error)
	UpdatePollFunc func(ctx context.Context, poll Poll) error
}

func (m *mockRepository) CreatePoll(ctx context.Context, poll Poll) error {
	panic("not implemented")
}

func (m *mockRepository) GetPoll(ctx context.Context, id uuid.UUID) (Poll, error) {
	return m.GetPollFunc(ctx, id)
}

func (m *mockRepository) GetPolls(ctx context.Context, limit int32, cursor *string) (GetPollsResponse, error) {
	panic("not implemented")
}

func (m *mockRepository) UpdatePoll(ctx context.Context, poll Poll) error {
	return m.UpdatePollFunc(ctx, poll)
}

func (m *mockRepository) DeletePoll(ctx context.Context, id uuid.UUID) error {
	panic("not implemented")
}

func (m *mockRepository) GetResults(ctx context.Context, pollID uuid.UUID) (Results, error) {
	panic("not implemented")
}

func (m *mockRepository) GetVoteRecord(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (VoteRecord, error) {
	panic("not implemented")
}

func (m *mockRepository) RecordVote(ctx context.Context, record VoteRecord) error {
	panic("not implemented")
}

func TestUpdatePoll(t *testing.T) {
	t.Run("updates poll and bumps version", func(t *testing.T) {
		existing := newTestPoll()

		repo := &mockRepository{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (Poll, error) {
				return existing, nil
			},
			UpdatePollFunc: func(ctx context.Context, poll Poll) error {
				return nil
			},
		}

		updatedInput := newTestPoll()
		updatedInput.Name = "New Name"

		updated, err := UpdatePoll(context.Background(), repo, existing.ID, updatedInput, existing.Version)
		assert.NoError(t, err)
		assert.Equal(t, existing.ID, updated.ID)
		assert.Equal(t, existing.Version+1, updated.Version)
		assert.Equal(t, "New Name", updated.Name)
	})

	t.Run("version mismatch returns conflict", func(t *testing.T) {
		existing := newTestPoll()
		existing.Version = 2

		repo := &mockRepository{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (Poll, error) {
				return existing, nil
			},
			UpdatePollFunc: func(ctx context.Context, poll Poll) error {
				return nil
			},
		}

		_, err := UpdatePoll(context.Background(), repo, existing.ID, newTestPoll(), 1)

		var pollsErr *Error
		assert.ErrorAs(t, err, &pollsErr)
		assert.Equal(t, REASON_VERSION_CONFLICT, pollsErr.Reason)
	})

	t.Run("invalid poll is rejected", func(t *testing.T) {
		existing := newTestPoll()

		repo := &mockRepository{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (Poll, error) {
				return existing, nil
			},
			UpdatePollFunc: func(ctx context.Context, poll Poll) error {
				return nil
			},
		}

		invalidInput := newTestPoll()
		invalidInput.Options = nil

		_, err := UpdatePoll(context.Background(), repo, existing.ID, invalidInput, existing.Version)
		assertInvalidPoll(t, err)
	})

	t.Run("propagates repo errors", func(t *testing.T) {
		repo := &mockRepository{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (Poll, error) {
				return Poll{}, NewPollDoesNotExistError("not found", nil)
			},
			UpdatePollFunc: func(ctx context.Context, poll Poll) error {
				return nil
			},
		}

		_, err := UpdatePoll(context.Background(), repo, uuid.New(), newTestPoll(), 1)

		var pollsErr *Error
		assert.ErrorAs(t, err, &pollsErr)
		assert.Equal(t, REASON_POLL_DOES_NOT_EXIST, pollsErr.Reason)
	})

	t.Run("propagates version conflicts", func(t *testing.T) {
		repo := &mockRepository{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (Poll, error) {
				return newTestPoll(), nil
			},
			UpdatePollFunc: func(ctx context.Context, poll Poll) error {
				return NewVersionConflictError("conflict", errors.New("conflict"))
			},
		}

		_, err := UpdatePoll(context.Background(), repo, uuid.New(), newTestPoll(), 1)

		var pollsErr *Error
		assert.ErrorAs(t, err, &pollsErr)
		assert.Equal(t, REASON_VERSION_CONFLICT, pollsErr.Reason)
	})
}
