package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/International-Combat-Archery-Alliance/voting-api/slices"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
)

func (a *API) GetVotingV1Polls(ctx context.Context, request GetVotingV1PollsRequestObject) (GetVotingV1PollsResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "GetVotingV1Polls")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// guaranteed to be non-nil from openapi doc
	limit := int32(*request.Params.Limit)

	result, err := a.db.GetPolls(ctx, limit, request.Params.Cursor)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to get polls from the DB", "error", err)

		var pollsErr *polls.Error
		if errors.As(err, &pollsErr) {
			switch pollsErr.Reason {
			case polls.REASON_INVALID_CURSOR:
				return GetVotingV1Polls400JSONResponse{
					Code:    InvalidCursor,
					Message: "Passed in cursor is invalid",
				}, nil
			}
		}
		return GetVotingV1Polls500JSONResponse{
			Code:    InternalError,
			Message: "Failed to get polls",
		}, nil
	}

	respPolls := []Poll{}
	for _, v := range result.Data {
		respPolls = append(respPolls, pollToApiPoll(v))
	}

	return GetVotingV1Polls200JSONResponse{
		Data:        respPolls,
		Cursor:      result.Cursor,
		HasNextPage: result.HasNextPage,
	}, nil
}

func (a *API) PostVotingV1Polls(ctx context.Context, request PostVotingV1PollsRequestObject) (PostVotingV1PollsResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "PostVotingV1Polls")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	id := uuid.New()
	request.Body.Id = &id

	// request.Body is guaranteed to be non-nil from openapi doc
	poll, err := apiPollToPoll(*request.Body)
	if err != nil {
		span.RecordError(err)
		logger.Warn("Failed to convert poll into core type", "error", err)

		return PostVotingV1Polls400JSONResponse{
			Code:    InvalidBody,
			Message: fmt.Sprintf("Invalid poll body: %s", err),
		}, nil
	}
	poll.Version = 1

	if err := polls.ValidatePoll(poll); err != nil {
		span.RecordError(err)
		logger.Warn("Invalid poll body", "error", err)

		return PostVotingV1Polls400JSONResponse{
			Code:    InvalidBody,
			Message: fmt.Sprintf("Invalid poll body: %s", err),
		}, nil
	}

	err = a.db.CreatePoll(ctx, poll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to create a poll", "error", err)

		return PostVotingV1Polls500JSONResponse{
			Code:    InternalError,
			Message: "Failed to create the poll",
		}, nil
	}

	logger.Info("created new poll", slog.String("poll-id", id.String()))

	return PostVotingV1Polls200JSONResponse(pollToApiPoll(poll)), nil
}

func (a *API) GetVotingV1PollsId(ctx context.Context, request GetVotingV1PollsIdRequestObject) (GetVotingV1PollsIdResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "GetVotingV1PollsId")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	poll, err := a.db.GetPoll(ctx, request.Id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to fetch a poll", "error", err)

		var pollsErr *polls.Error
		if errors.As(err, &pollsErr) {
			switch pollsErr.Reason {
			case polls.REASON_POLL_DOES_NOT_EXIST:
				return GetVotingV1PollsId404JSONResponse{
					Code:    NotFound,
					Message: "Poll does not exist",
				}, nil
			}
		}

		return GetVotingV1PollsId500JSONResponse{
			Code:    InternalError,
			Message: "Failed to get poll",
		}, nil
	}

	return GetVotingV1PollsId200JSONResponse{Poll: pollToApiPoll(poll)}, nil
}

func (a *API) PatchVotingV1PollsId(ctx context.Context, request PatchVotingV1PollsIdRequestObject) (PatchVotingV1PollsIdResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "PatchVotingV1PollsId")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	request.Body.Id = &request.Id

	poll, err := apiPollToPoll(*request.Body)
	if err != nil {
		span.RecordError(err)
		logger.Warn("Invalid poll body", slog.String("error", err.Error()))

		return PatchVotingV1PollsId400JSONResponse{
			Code:    InvalidBody,
			Message: fmt.Sprintf("Invalid poll body: %s", err),
		}, nil
	}

	updatedPoll, err := polls.UpdatePoll(ctx, a.db, request.Id, poll, request.Params.Version)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("failed to update poll", slog.String("error", err.Error()))

		var pollsErr *polls.Error
		if errors.As(err, &pollsErr) {
			switch pollsErr.Reason {
			case polls.REASON_POLL_DOES_NOT_EXIST:
				return PatchVotingV1PollsId404JSONResponse{
					Code:    NotFound,
					Message: "Poll not found",
				}, nil
			case polls.REASON_INVALID_POLL:
				return PatchVotingV1PollsId400JSONResponse{
					Code:    InvalidBody,
					Message: fmt.Sprintf("Invalid poll body: %s", err),
				}, nil
			case polls.REASON_VERSION_CONFLICT:
				return PatchVotingV1PollsId409JSONResponse{
					Code:    VersionConflict,
					Message: "Poll was modified by another request, retry with the latest version",
				}, nil
			}
		}

		return PatchVotingV1PollsId500JSONResponse{
			Code:    InternalError,
			Message: "Updating poll failed",
		}, nil
	}

	return PatchVotingV1PollsId200JSONResponse{Poll: pollToApiPoll(updatedPoll)}, nil
}

func (a *API) DeleteVotingV1PollsId(ctx context.Context, request DeleteVotingV1PollsIdRequestObject) (DeleteVotingV1PollsIdResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "DeleteVotingV1PollsId")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err := a.db.DeletePoll(ctx, request.Id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("failed to delete poll", slog.String("error", err.Error()))

		var pollsErr *polls.Error
		if errors.As(err, &pollsErr) {
			switch pollsErr.Reason {
			case polls.REASON_POLL_DOES_NOT_EXIST:
				return DeleteVotingV1PollsId404JSONResponse{
					Code:    NotFound,
					Message: "Poll not found",
				}, nil
			}
		}

		return DeleteVotingV1PollsId500JSONResponse{
			Code:    InternalError,
			Message: "Deleting poll failed",
		}, nil
	}

	return DeleteVotingV1PollsId204Response{}, nil
}

func pollToApiPoll(poll polls.Poll) Poll {
	publicResultsLevel := PublicResultsLevel(poll.PublicResultsLevel)
	return Poll{
		Id:                &poll.ID,
		Version:           &poll.Version,
		Name:              poll.Name,
		Description:       poll.Description,
		StartTime:         poll.StartTime,
		EndTime:           poll.EndTime,
		ResultsVisibility: ResultsVisibility(poll.ResultsVisibility),
		PublicResultsLevel: &publicResultsLevel,
		VoteConfig: &VoteConfig{
			MaxSelections:         &poll.VoteConfig.MaxSelections,
			MaxSelectionsPerGroup: poll.VoteConfig.MaxSelectionsPerGroup,
		},
		Groups: slicePtr(slices.Map(poll.Groups, func(g polls.Group) PollGroup {
			groupOptions := []PollOption{}
			for _, o := range poll.Options {
				if o.GroupID != nil && *o.GroupID == g.ID {
					groupOptions = append(groupOptions, optionToApiOption(o))
				}
			}
			return PollGroup{
				Id:       &g.ID,
				Name:     g.Name,
				Color:    g.Color,
				ImageUrl: g.ImageURL,
				Options:  groupOptions,
			}
		})),
		Options: slicePtr(func() []PollOption {
			ungrouped := []PollOption{}
			for _, o := range poll.Options {
				if o.GroupID == nil {
					ungrouped = append(ungrouped, optionToApiOption(o))
				}
			}
			return ungrouped
		}()),
		Status: statusToApiStatus(poll.Status(time.Now())),
	}
}

func optionToApiOption(o polls.Option) PollOption {
	return PollOption{
		Id:       &o.ID,
		Name:     o.Name,
		Subtitle: o.Subtitle,
		ImageUrl: o.ImageURL,
	}
}

// apiPollToPoll converts an API poll to a core poll. IDs for the poll, groups,
// and options are assigned if they are missing. The version is left as 0 and
// expected to be set by the caller.
func apiPollToPoll(poll Poll) (polls.Poll, error) {
	id := uuid.Nil
	if poll.Id != nil {
		id = *poll.Id
	}

	visibility := polls.ResultsVisibility(poll.ResultsVisibility)
	switch visibility {
	case polls.RESULTS_VISIBILITY_LIVE, polls.RESULTS_VISIBILITY_AFTER_CLOSE, polls.RESULTS_VISIBILITY_ADMIN_ONLY:
	default:
		return polls.Poll{}, fmt.Errorf("unknown results visibility %q", poll.ResultsVisibility)
	}

	publicResultsLevel := polls.PUBLIC_RESULTS_LEVEL_FULL
	if poll.PublicResultsLevel != nil {
		level := polls.PublicResultsLevel(*poll.PublicResultsLevel)
		switch level {
		case polls.PUBLIC_RESULTS_LEVEL_FULL, polls.PUBLIC_RESULTS_LEVEL_PERCENTAGES, polls.PUBLIC_RESULTS_LEVEL_RANKINGS, polls.PUBLIC_RESULTS_LEVEL_NONE:
			publicResultsLevel = level
		default:
			return polls.Poll{}, fmt.Errorf("unknown public results level %q", *poll.PublicResultsLevel)
		}
	}

	voteConfig := polls.VoteConfig{MaxSelections: 1}
	if poll.VoteConfig != nil {
		if poll.VoteConfig.MaxSelections != nil {
			voteConfig.MaxSelections = *poll.VoteConfig.MaxSelections
		}
		voteConfig.MaxSelectionsPerGroup = poll.VoteConfig.MaxSelectionsPerGroup
	}

	groups := []polls.Group{}
	options := []polls.Option{}
	for _, g := range derefSlice(poll.Groups) {
		groupID := uuid.New()
		if g.Id != nil {
			groupID = *g.Id
		}
		groups = append(groups, polls.Group{
			ID:       groupID,
			Name:     g.Name,
			Color:    g.Color,
			ImageURL: g.ImageUrl,
		})
		for _, o := range g.Options {
			options = append(options, apiOptionToOption(o, &groupID))
		}
	}
	for _, o := range derefSlice(poll.Options) {
		options = append(options, apiOptionToOption(o, nil))
	}

	return polls.Poll{
		ID:                 id,
		Name:               poll.Name,
		Description:        poll.Description,
		StartTime:          poll.StartTime,
		EndTime:            poll.EndTime,
		ResultsVisibility:  visibility,
		PublicResultsLevel: publicResultsLevel,
		VoteConfig:         voteConfig,
		Groups:            groups,
		Options:           options,
	}, nil
}

func apiOptionToOption(o PollOption, groupID *uuid.UUID) polls.Option {
	optionID := uuid.New()
	if o.Id != nil {
		optionID = *o.Id
	}
	return polls.Option{
		ID:       optionID,
		GroupID:  groupID,
		Name:     o.Name,
		Subtitle: o.Subtitle,
		ImageURL: o.ImageUrl,
	}
}

func statusToApiStatus(status polls.Status) *PollStatus {
	var apiStatus PollStatus
	switch status {
	case polls.STATUS_UPCOMING:
		apiStatus = Upcoming
	case polls.STATUS_ACTIVE:
		apiStatus = Active
	case polls.STATUS_CLOSED:
		apiStatus = Closed
	}
	return &apiStatus
}

func slicePtr[T any](s []T) *[]T {
	return &s
}

func derefSlice[T any](s *[]T) []T {
	if s == nil {
		return nil
	}
	return *s
}
