package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/International-Combat-Archery-Alliance/captcha"
	"github.com/International-Combat-Archery-Alliance/middleware"
	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActiveDomainPoll() polls.Poll {
	return polls.Poll{
		ID:                uuid.New(),
		Version:           1,
		Name:              "Eastern Finals MVP",
		StartTime:         time.Now().Add(-time.Hour),
		EndTime:           time.Now().Add(time.Hour),
		ResultsVisibility: polls.RESULTS_VISIBILITY_LIVE,
		VoteConfig:        polls.VoteConfig{MaxSelections: 1},
		Options: []polls.Option{
			{ID: uuid.New(), Name: "Cameron Cardwell"},
			{ID: uuid.New(), Name: "Nate Langh"},
		},
	}
}

func newVoteRequest(pollID uuid.UUID, key string, optionIDs ...uuid.UUID) PostVotingV1PollsIdVotesRequestObject {
	return PostVotingV1PollsIdVotesRequestObject{
		Id: openapi_types.UUID(pollID),
		Params: PostVotingV1PollsIdVotesParams{
			CfTurnstileResponse: "valid-token",
			IdempotencyKey:      openapi_types.UUID(uuid.MustParse(key)),
		},
		Body: &VoteBallot{
			OptionIds: uuidsToOpenapiUUIDs(optionIDs),
		},
	}
}

func voteNotFoundDB(poll polls.Poll) *mockDB {
	return &mockDB{
		GetVoteRecordFunc: func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
			return polls.VoteRecord{}, polls.NewVoteRecordNotFoundError("not found")
		},
		GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
			return poll, nil
		},
		RecordVoteFunc: func(ctx context.Context, record polls.VoteRecord) error {
			return nil
		},
	}
}

func TestPostVotingV1PollsIdVotes(t *testing.T) {
	t.Run("successfully records a vote", func(t *testing.T) {
		poll := newActiveDomainPoll()
		mock := voteNotFoundDB(poll)
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		key := uuid.NewString()
		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, key, poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, openapi_types.UUID(poll.ID), r.PollId)
		assert.Equal(t, []openapi_types.UUID{openapi_types.UUID(poll.Options[0].ID)}, r.OptionIds)
	})

	t.Run("replay of the same ballot returns the recorded vote without calling captcha", func(t *testing.T) {
		poll := newActiveDomainPoll()
		key := uuid.NewString()

		captchaCalled := false
		mockCaptcha := &mockCaptchaValidator{
			ValidateFunc: func(ctx context.Context, token string, remoteIP string) (captcha.ValidatedData, error) {
				captchaCalled = true
				return &mockCaptchaValidatedData{}, nil
			},
		}

		mock := &mockDB{
			GetVoteRecordFunc: func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
				return polls.VoteRecord{
					PollID:         pollID,
					IdempotencyKey: key,
					OptionIDs:      []uuid.UUID{poll.Options[0].ID},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), mockCaptcha, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, key, poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, []openapi_types.UUID{openapi_types.UUID(poll.Options[0].ID)}, r.OptionIds)
		assert.False(t, captchaCalled, "captcha should not be validated for a replayed request")
	})

	t.Run("same key with a different ballot is a conflict", func(t *testing.T) {
		poll := newActiveDomainPoll()
		key := uuid.NewString()

		mock := &mockDB{
			GetVoteRecordFunc: func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
				return polls.VoteRecord{
					PollID:         pollID,
					IdempotencyKey: key,
					OptionIDs:      []uuid.UUID{poll.Options[0].ID},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, key, poll.Options[1].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes409JSONResponse)
		require.True(t, ok)
		assert.Equal(t, IdempotencyConflict, r.Code)
	})

	t.Run("invalid captcha", func(t *testing.T) {
		poll := newActiveDomainPoll()
		mock := voteNotFoundDB(poll)
		mockCaptcha := &mockCaptchaValidator{
			ValidateFunc: func(ctx context.Context, token string, remoteIP string) (captcha.ValidatedData, error) {
				return nil, errors.New("invalid captcha")
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), mockCaptcha, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, uuid.NewString(), poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes400JSONResponse)
		require.True(t, ok)
		assert.Equal(t, CaptchaInvalid, r.Code)
	})

	t.Run("poll not found", func(t *testing.T) {
		poll := newActiveDomainPoll()
		mock := voteNotFoundDB(poll)
		mock.GetPollFunc = func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
			return polls.Poll{}, polls.NewPollDoesNotExistError("not found", nil)
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, uuid.NewString(), poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes404JSONResponse)
		require.True(t, ok)
		assert.Equal(t, NotFound, r.Code)
	})

	t.Run("poll is not active", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.StartTime = time.Now().Add(time.Hour)
		mock := voteNotFoundDB(poll)
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, uuid.NewString(), poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes403JSONResponse)
		require.True(t, ok)
		assert.Equal(t, PollNotActive, r.Code)
	})

	t.Run("invalid ballot", func(t *testing.T) {
		poll := newActiveDomainPoll()
		mock := voteNotFoundDB(poll)
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		// MaxSelections is 1, voting for 2 options
		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, uuid.NewString(), poll.Options[0].ID, poll.Options[1].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes400JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InvalidBallot, r.Code)
	})

	t.Run("losing a race with the same key is treated as a replay", func(t *testing.T) {
		poll := newActiveDomainPoll()
		key := uuid.NewString()

		mock := &mockDB{
			GetVoteRecordFunc: func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
				return polls.VoteRecord{}, polls.NewVoteRecordNotFoundError("not found")
			},
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			RecordVoteFunc: func(ctx context.Context, record polls.VoteRecord) error {
				return polls.NewVoteAlreadyRecordedError("already recorded", errors.New("condition check failed"))
			},
		}
		// After the race is lost, the record exists
		mock.GetVoteRecordFunc = sequenceVoteRecordGetter(
			polls.NewVoteRecordNotFoundError("not found"),
			polls.VoteRecord{PollID: poll.ID, IdempotencyKey: key, OptionIDs: []uuid.UUID{poll.Options[0].ID}},
		)

		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, key, poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, []openapi_types.UUID{openapi_types.UUID(poll.Options[0].ID)}, r.OptionIds)
	})

	t.Run("db failure recording the vote", func(t *testing.T) {
		poll := newActiveDomainPoll()
		mock := voteNotFoundDB(poll)
		mock.RecordVoteFunc = func(ctx context.Context, record polls.VoteRecord) error {
			return polls.NewFailedToWriteError("boom", errors.New("boom"))
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.PostVotingV1PollsIdVotes(context.Background(), newVoteRequest(poll.ID, uuid.NewString(), poll.Options[0].ID))

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1PollsIdVotes500JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InternalError, r.Code)
	})
}

// sequenceVoteRecordGetter returns GetVoteRecord responses in order per call
func sequenceVoteRecordGetter(err error, record polls.VoteRecord) func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
	calls := 0
	return func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
		calls++
		if calls == 1 {
			return polls.VoteRecord{}, err
		}
		return record, nil
	}
}

func TestGetVotingV1PollsIdResults(t *testing.T) {
	adminCtx := middleware.CtxWithJWT(context.Background(), &mockAuthToken{email: "admin@icaa.world", isAdmin: true})

	t.Run("live results are public", func(t *testing.T) {
		poll := newActiveDomainPoll()
		deletedOptionID := uuid.New()
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{
					PollID:     pollID,
					TotalVotes: 5,
					Counts: map[uuid.UUID]int{
						poll.Options[0].ID: 3,
						poll.Options[1].ID: 2,
						// Stale count of a deleted option should be filtered out
						deletedOptionID: 10,
					},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, Full, r.Level)
		require.NotNil(t, r.TotalVotes)
		assert.Equal(t, 5, *r.TotalVotes)
		require.Len(t, r.Results, 2)
		for _, result := range r.Results {
			assert.NotEqual(t, openapi_types.UUID(deletedOptionID), result.OptionId)
		}
	})

	t.Run("poll not found", func(t *testing.T) {
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return polls.Poll{}, polls.NewPollDoesNotExistError("not found", nil)
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(uuid.New()),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults404JSONResponse)
		require.True(t, ok)
		assert.Equal(t, NotFound, r.Code)
	})

	t.Run("admin only results are hidden from the public", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.ResultsVisibility = polls.RESULTS_VISIBILITY_ADMIN_ONLY
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults403JSONResponse)
		require.True(t, ok)
		assert.Equal(t, AuthError, r.Code)
	})

	t.Run("admin only results are visible to admins", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.ResultsVisibility = polls.RESULTS_VISIBILITY_ADMIN_ONLY
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{PollID: pollID, TotalVotes: 1, Counts: map[uuid.UUID]int{poll.Options[0].ID: 1}}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(adminCtx, GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
		require.NotNil(t, r.TotalVotes)
		assert.Equal(t, 1, *r.TotalVotes)
	})

	t.Run("after close results are hidden before the poll closes", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.ResultsVisibility = polls.RESULTS_VISIBILITY_AFTER_CLOSE
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		_, ok := resp.(GetVotingV1PollsIdResults403JSONResponse)
		require.True(t, ok)
	})

	t.Run("after close results are public after the poll closes", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.ResultsVisibility = polls.RESULTS_VISIBILITY_AFTER_CLOSE
		poll.StartTime = time.Now().Add(-2 * time.Hour)
		poll.EndTime = time.Now().Add(-time.Hour)
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{PollID: pollID, Counts: map[uuid.UUID]int{}}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		_, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
	})

	t.Run("percentage level returns percentages without total votes", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.PublicResultsLevel = polls.PUBLIC_RESULTS_LEVEL_PERCENTAGES
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{
					PollID:     pollID,
					TotalVotes: 10,
					Counts: map[uuid.UUID]int{
						poll.Options[0].ID: 6,
						poll.Options[1].ID: 4,
					},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, Percentages, r.Level)
		assert.Nil(t, r.TotalVotes)
		require.Len(t, r.Results, 2)
		assert.NotNil(t, r.Results[0].Percentage)
		assert.Nil(t, r.Results[0].Count)
		assert.Nil(t, r.Results[0].Rank)
	})

	t.Run("ranking level returns ranks", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.PublicResultsLevel = polls.PUBLIC_RESULTS_LEVEL_RANKINGS
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{
					PollID: pollID,
					Counts: map[uuid.UUID]int{
						poll.Options[0].ID: 3,
						poll.Options[1].ID: 1,
					},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, Rankings, r.Level)
		assert.Nil(t, r.TotalVotes)
		require.Len(t, r.Results, 2)
		assert.NotNil(t, r.Results[0].Rank)
		assert.Nil(t, r.Results[0].Count)
		assert.Nil(t, r.Results[0].Percentage)
	})

	t.Run("none level returns 403 for public", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.PublicResultsLevel = polls.PUBLIC_RESULTS_LEVEL_NONE
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(context.Background(), GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults403JSONResponse)
		require.True(t, ok)
		assert.Equal(t, AuthError, r.Code)
	})

	t.Run("none level returns full results for admin", func(t *testing.T) {
		poll := newActiveDomainPoll()
		poll.PublicResultsLevel = polls.PUBLIC_RESULTS_LEVEL_NONE
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return poll, nil
			},
			GetResultsFunc: func(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
				return polls.Results{
					PollID:     pollID,
					TotalVotes: 5,
					Counts: map[uuid.UUID]int{
						poll.Options[0].ID: 3,
						poll.Options[1].ID: 2,
					},
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenService(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsIdResults(adminCtx, GetVotingV1PollsIdResultsRequestObject{
			Id: openapi_types.UUID(poll.ID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsIdResults200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, Full, r.Level)
		require.NotNil(t, r.TotalVotes)
		assert.Equal(t, 5, *r.TotalVotes)
	})
}
