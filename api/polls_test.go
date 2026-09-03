package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/International-Combat-Archery-Alliance/voting-api/ptr"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestApiPoll() Poll {
	startTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	endTime := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	return Poll{
		Name:              "Eastern Finals MVP",
		StartTime:         startTime,
		EndTime:           endTime,
		ResultsVisibility: AfterClose,
		Options: &[]PollOption{
			{Name: "Cameron Cardwell"},
			{Name: "Nate Langh"},
		},
	}
}

func TestGetVotingV1Polls(t *testing.T) {
	t.Run("successfully gets polls", func(t *testing.T) {
		mock := &mockDB{
			GetPollsFunc: func(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error) {
				return polls.GetPollsResponse{
					Data:        []polls.Poll{{ID: uuid.New(), Name: "Poll 1", StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)}},
					HasNextPage: false,
				}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1Polls(context.Background(), GetVotingV1PollsRequestObject{
			Params: GetVotingV1PollsParams{
				Limit: ptr.Int(10),
			},
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1Polls200JSONResponse)
		require.True(t, ok)
		assert.Len(t, r.Data, 1)
	})

	t.Run("invalid cursor", func(t *testing.T) {
		mock := &mockDB{
			GetPollsFunc: func(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error) {
				return polls.GetPollsResponse{}, polls.NewInvalidCursorError("invalid cursor", errors.New("invalid cursor"))
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1Polls(context.Background(), GetVotingV1PollsRequestObject{
			Params: GetVotingV1PollsParams{
				Limit:  ptr.Int(10),
				Cursor: ptr.String("bad-cursor"),
			},
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1Polls400JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InvalidCursor, r.Code)
	})

	t.Run("unknown db error", func(t *testing.T) {
		mock := &mockDB{
			GetPollsFunc: func(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error) {
				return polls.GetPollsResponse{}, errors.New("boom")
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1Polls(context.Background(), GetVotingV1PollsRequestObject{
			Params: GetVotingV1PollsParams{
				Limit: ptr.Int(10),
			},
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1Polls500JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InternalError, r.Code)
	})
}

func TestPostVotingV1Polls(t *testing.T) {
	t.Run("successfully creates a poll and assigns ids", func(t *testing.T) {
		mock := &mockDB{
			CreatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		resp, err := api.PostVotingV1Polls(context.Background(), PostVotingV1PollsRequestObject{Body: &body})

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1Polls200JSONResponse)
		require.True(t, ok)
		assert.NotEqual(t, uuid.Nil, *r.Id)
		assert.Equal(t, 1, *r.Version)
		assert.NotNil(t, r.VoteConfig)
		assert.Equal(t, 1, *r.VoteConfig.MaxSelections)
		for _, o := range *r.Options {
			assert.NotEqual(t, uuid.Nil, *o.Id)
		}
		assert.Equal(t, Active, *r.Status)
	})

	t.Run("creates a poll with nested groups and assigns all ids", func(t *testing.T) {
		var createdPoll polls.Poll
		mock := &mockDB{
			CreatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				createdPoll = poll
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		body.Groups = &[]PollGroup{
			{
				Name:  "Team Boston",
				Color: ptr.String("#70b2e0"),
				Options: []PollOption{
					{Name: "Cameron Cardwell", Subtitle: ptr.String("#17")},
					{Name: "Nate Langh", Subtitle: ptr.String("#3")},
				},
			},
		}
		resp, err := api.PostVotingV1Polls(context.Background(), PostVotingV1PollsRequestObject{Body: &body})

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1Polls200JSONResponse)
		require.True(t, ok)

		require.Len(t, *r.Groups, 1)
		group := (*r.Groups)[0]
		assert.NotEqual(t, uuid.Nil, *group.Id)
		require.Len(t, group.Options, 2)
		for _, o := range group.Options {
			assert.NotEqual(t, uuid.Nil, *o.Id)
		}

		// the flat domain model wires up the group ID on each nested option
		require.Len(t, createdPoll.Options, 4)
		for _, o := range createdPoll.Options[:2] {
			require.NotNil(t, o.GroupID)
			assert.Equal(t, *group.Id, *o.GroupID)
		}
		for _, o := range createdPoll.Options[2:] {
			assert.Nil(t, o.GroupID)
		}
	})

	t.Run("invalid poll body", func(t *testing.T) {
		mock := &mockDB{
			CreatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		body.EndTime = body.StartTime // end time must be after start time
		resp, err := api.PostVotingV1Polls(context.Background(), PostVotingV1PollsRequestObject{Body: &body})

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1Polls400JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InvalidBody, r.Code)
	})

	t.Run("unknown results visibility", func(t *testing.T) {
		mock := &mockDB{
			CreatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		body.ResultsVisibility = "Bogus"
		resp, err := api.PostVotingV1Polls(context.Background(), PostVotingV1PollsRequestObject{Body: &body})

		require.NoError(t, err)
		r, ok := resp.(PostVotingV1Polls400JSONResponse)
		require.True(t, ok)
		assert.Equal(t, InvalidBody, r.Code)
	})
}

func TestGetVotingV1PollsId(t *testing.T) {
	t.Run("successfully gets a poll", func(t *testing.T) {
		pollID := uuid.New()
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return polls.Poll{ID: pollID, Name: "Poll", StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)}, nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsId(context.Background(), GetVotingV1PollsIdRequestObject{
			Id: openapi_types.UUID(pollID),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsId200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, pollID, *r.Poll.Id)
	})

	t.Run("poll not found", func(t *testing.T) {
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return polls.Poll{}, polls.NewPollDoesNotExistError("not found", nil)
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.GetVotingV1PollsId(context.Background(), GetVotingV1PollsIdRequestObject{
			Id: openapi_types.UUID(uuid.New()),
		})

		require.NoError(t, err)
		r, ok := resp.(GetVotingV1PollsId404JSONResponse)
		require.True(t, ok)
		assert.Equal(t, NotFound, r.Code)
	})
}

func TestPatchVotingV1PollsId(t *testing.T) {
	t.Run("successfully updates a poll", func(t *testing.T) {
		pollID := uuid.New()
		existing := polls.Poll{
			ID:                pollID,
			Version:           3,
			Name:              "Old Name",
			StartTime:         time.Now(),
			EndTime:           time.Now().Add(time.Hour),
			ResultsVisibility: polls.RESULTS_VISIBILITY_LIVE,
			VoteConfig:        polls.VoteConfig{MaxSelections: 1},
			Options:           []polls.Option{{ID: uuid.New(), Name: "Option 1"}},
		}
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return existing, nil
			},
			UpdatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		resp, err := api.PatchVotingV1PollsId(context.Background(), PatchVotingV1PollsIdRequestObject{
			Id:   openapi_types.UUID(pollID),
			Params: PatchVotingV1PollsIdParams{Version: existing.Version},
			Body: &body,
		})

		require.NoError(t, err)
		r, ok := resp.(PatchVotingV1PollsId200JSONResponse)
		require.True(t, ok)
		assert.Equal(t, pollID, *r.Poll.Id)
		assert.Equal(t, 4, *r.Poll.Version)
	})

	t.Run("poll not found", func(t *testing.T) {
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return polls.Poll{}, polls.NewPollDoesNotExistError("not found", nil)
			},
			UpdatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		resp, err := api.PatchVotingV1PollsId(context.Background(), PatchVotingV1PollsIdRequestObject{
			Id:   openapi_types.UUID(uuid.New()),
			Params: PatchVotingV1PollsIdParams{Version: 1},
			Body: &body,
		})

		require.NoError(t, err)
		r, ok := resp.(PatchVotingV1PollsId404JSONResponse)
		require.True(t, ok)
		assert.Equal(t, NotFound, r.Code)
	})

	t.Run("version conflict", func(t *testing.T) {
		mock := &mockDB{
			GetPollFunc: func(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
				return polls.Poll{ID: id, Version: 1}, nil
			},
			UpdatePollFunc: func(ctx context.Context, poll polls.Poll) error {
				return polls.NewVersionConflictError("conflict", errors.New("conflict"))
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		body := newTestApiPoll()
		resp, err := api.PatchVotingV1PollsId(context.Background(), PatchVotingV1PollsIdRequestObject{
			Id:   openapi_types.UUID(uuid.New()),
			Params: PatchVotingV1PollsIdParams{Version: 1},
			Body: &body,
		})

		require.NoError(t, err)
		r, ok := resp.(PatchVotingV1PollsId409JSONResponse)
		require.True(t, ok)
		assert.Equal(t, VersionConflict, r.Code)
	})
}

func TestDeleteVotingV1PollsId(t *testing.T) {
	t.Run("successfully deletes a poll", func(t *testing.T) {
		mock := &mockDB{
			DeletePollFunc: func(ctx context.Context, id uuid.UUID) error {
				return nil
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.DeleteVotingV1PollsId(context.Background(), DeleteVotingV1PollsIdRequestObject{
			Id: openapi_types.UUID(uuid.New()),
		})

		require.NoError(t, err)
		_, ok := resp.(DeleteVotingV1PollsId204Response)
		require.True(t, ok)
	})

	t.Run("poll not found", func(t *testing.T) {
		mock := &mockDB{
			DeletePollFunc: func(ctx context.Context, id uuid.UUID) error {
				return polls.NewPollDoesNotExistError("not found", nil)
			},
		}
		api := NewAPI(mock, noopLogger, LOCAL, newTestTokenValidator(), &mockCaptchaValidator{}, func(context.Context) error { return nil })

		resp, err := api.DeleteVotingV1PollsId(context.Background(), DeleteVotingV1PollsIdRequestObject{
			Id: openapi_types.UUID(uuid.New()),
		})

		require.NoError(t, err)
		r, ok := resp.(DeleteVotingV1PollsId404JSONResponse)
		require.True(t, ok)
		assert.Equal(t, NotFound, r.Code)
	})
}
