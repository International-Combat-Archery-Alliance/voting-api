package dynamo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/International-Combat-Archery-Alliance/voting-api/ptr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPoll(t *testing.T) polls.Poll {
	t.Helper()

	groupID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	return polls.Poll{
		ID:                uuid.New(),
		Version:           1,
		Name:              "Eastern Finals MVP",
		Description:       ptr.String("Vote for the MVP"),
		StartTime:         time.Date(2026, 8, 7, 19, 0, 0, 0, time.UTC),
		EndTime:           time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		ResultsVisibility: polls.RESULTS_VISIBILITY_AFTER_CLOSE,
		VoteConfig: polls.VoteConfig{
			MaxSelections:         2,
			MaxSelectionsPerGroup: ptr.Int(1),
		},
		Groups: []polls.Group{
			{
				ID:       groupID,
				Name:     "Team Boston",
				Color:    ptr.String("#70b2e0"),
				ImageURL: ptr.String("https://assets.icaa.world/logo.png"),
			},
		},
		Options: []polls.Option{
			{
				ID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				GroupID:  &groupID,
				Name:     "Cameron Cardwell",
				Subtitle: ptr.String("#17"),
				ImageURL: ptr.String("https://assets.icaa.world/cam.jpg"),
			},
			{
				ID:   uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Name: "Nate Langh",
			},
		},
	}
}

func TestCreateAndGetPoll(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)

	err := db.CreatePoll(context.Background(), poll)
	require.NoError(t, err)

	fetchedPoll, err := db.GetPoll(context.Background(), poll.ID)
	require.NoError(t, err)

	assert.Equal(t, poll, fetchedPoll)
}

func TestCreatePollThatAlreadyExists(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)

	err := db.CreatePoll(context.Background(), poll)
	require.NoError(t, err)

	err = db.CreatePoll(context.Background(), poll)

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_POLL_ALREADY_EXISTS, pollsErr.Reason)
}

func TestGetPollThatDoesNotExist(t *testing.T) {
	resetTable(context.Background())

	_, err := db.GetPoll(context.Background(), uuid.New())

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_POLL_DOES_NOT_EXIST, pollsErr.Reason)
}

func TestGetPolls(t *testing.T) {
	resetTable(context.Background())

	pollsToMake := []polls.Poll{}
	for i := range 25 {
		poll := newTestPoll(t)
		poll.Name = fmt.Sprintf("Poll %d", i)
		// Make each poll start a day later so we can test the sort order
		poll.StartTime = poll.StartTime.Add(time.Duration(i) * 24 * time.Hour)
		poll.EndTime = poll.EndTime.Add(time.Duration(i) * 24 * time.Hour)
		pollsToMake = append(pollsToMake, poll)

		require.NoError(t, db.CreatePoll(context.Background(), poll))
	}

	t.Run("gets all polls newest start time first", func(t *testing.T) {
		limit := int32(50)
		result, err := db.GetPolls(context.Background(), limit, nil)
		require.NoError(t, err)

		assert.Len(t, result.Data, 25)
		assert.False(t, result.HasNextPage)
		assert.Nil(t, result.Cursor)

		// Newest start time first
		assert.Equal(t, pollsToMake[24].ID, result.Data[0].ID)
		assert.Equal(t, pollsToMake[0].ID, result.Data[24].ID)
	})

	t.Run("paginates with cursors", func(t *testing.T) {
		limit := int32(10)

		page1, err := db.GetPolls(context.Background(), limit, nil)
		require.NoError(t, err)
		assert.Len(t, page1.Data, 10)
		assert.True(t, page1.HasNextPage)
		require.NotNil(t, page1.Cursor)
		assert.Equal(t, pollsToMake[24].ID, page1.Data[0].ID)

		page2, err := db.GetPolls(context.Background(), limit, page1.Cursor)
		require.NoError(t, err)
		assert.Len(t, page2.Data, 10)
		assert.True(t, page2.HasNextPage)
		require.NotNil(t, page2.Cursor)

		page3, err := db.GetPolls(context.Background(), limit, page2.Cursor)
		require.NoError(t, err)
		assert.Len(t, page3.Data, 5)
		assert.False(t, page3.HasNextPage)

		seen := map[uuid.UUID]struct{}{}
		for _, p := range append(append(page1.Data, page2.Data...), page3.Data...) {
			seen[p.ID] = struct{}{}
		}
		assert.Len(t, seen, 25)
	})

	t.Run("invalid cursor", func(t *testing.T) {
		_, err := db.GetPolls(context.Background(), 10, ptr.String("not-a-cursor"))

		var pollsErr *polls.Error
		assert.ErrorAs(t, err, &pollsErr)
		assert.Equal(t, polls.REASON_INVALID_CURSOR, pollsErr.Reason)
	})
}

func TestUpdatePoll(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))

	poll.Version = 2
	poll.Name = "Updated Name"
	poll.Options = append(poll.Options, polls.Option{ID: uuid.New(), Name: "New Option"})

	require.NoError(t, db.UpdatePoll(context.Background(), poll))

	fetchedPoll, err := db.GetPoll(context.Background(), poll.ID)
	require.NoError(t, err)
	assert.Equal(t, poll, fetchedPoll)
}

func TestUpdatePollVersionConflict(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))

	poll.Version = 2
	require.NoError(t, db.UpdatePoll(context.Background(), poll))

	// Trying to update with a stale version fails
	stalePoll := poll
	stalePoll.Version = 2
	err := db.UpdatePoll(context.Background(), stalePoll)

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_VERSION_CONFLICT, pollsErr.Reason)
}

func TestDeletePoll(t *testing.T) {
	resetTable(context.Background())

	poll := newTestPoll(t)
	require.NoError(t, db.CreatePoll(context.Background(), poll))

	require.NoError(t, db.RecordVote(context.Background(), polls.VoteRecord{
		PollID:         poll.ID,
		IdempotencyKey: uuid.NewString(),
		OptionIDs:      []uuid.UUID{poll.Options[0].ID},
		CreatedAt:      time.Now(),
		TTL:            time.Now().Add(time.Hour),
	}))

	require.NoError(t, db.DeletePoll(context.Background(), poll.ID))

	_, err := db.GetPoll(context.Background(), poll.ID)
	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_POLL_DOES_NOT_EXIST, pollsErr.Reason)

	// Results are deleted too
	results, err := db.GetResults(context.Background(), poll.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, results.TotalVotes)
}

func TestDeletePollThatDoesNotExist(t *testing.T) {
	resetTable(context.Background())

	err := db.DeletePoll(context.Background(), uuid.New())

	var pollsErr *polls.Error
	assert.ErrorAs(t, err, &pollsErr)
	assert.Equal(t, polls.REASON_POLL_DOES_NOT_EXIST, pollsErr.Reason)
}
