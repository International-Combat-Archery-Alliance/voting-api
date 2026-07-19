package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel/codes"
)

// voteRecordTTL is how long idempotency records are kept in the DB.
// Retries realistically happen within minutes of the original request.
const voteRecordTTL = 24 * time.Hour

func (a *API) PostVotingV1PollsIdVotes(ctx context.Context, request PostVotingV1PollsIdVotesRequestObject) (PostVotingV1PollsIdVotesResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "PostVotingV1PollsIdVotes")
	defer span.End()

	logger := a.getLoggerOrBaseLogger(ctx)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// request.Body is guaranteed to be non-nil from openapi doc
	optionIDs := request.Body.OptionIds

	// Check the idempotency key before anything else so retried requests
	// short circuit without needing a fresh captcha token
	record, err := a.db.GetVoteRecord(ctx, request.Id, request.Params.IdempotencyKey.String())
	if err == nil {
		return voteRecordReplayResponse(record, optionIDs)
	}

	var pollsErr *polls.Error
	if !errors.As(err, &pollsErr) || pollsErr.Reason != polls.REASON_VOTE_RECORD_NOT_FOUND {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to fetch vote record", "error", err)

		return PostVotingV1PollsIdVotes500JSONResponse{
			Code:    InternalError,
			Message: "Failed to vote",
		}, nil
	}

	validatedData, err := a.captchaValidator.Validate(ctx, request.Params.CfTurnstileResponse, "")
	if err != nil {
		span.RecordError(err)
		logger.Warn("Invalid captcha", slog.String("error", err.Error()))

		return PostVotingV1PollsIdVotes400JSONResponse{
			Code:    CaptchaInvalid,
			Message: "Invalid captcha",
		}, nil
	}
	if a.env == PROD && validatedData.Hostname() != "icaa.world" {
		logger.Warn("Invalid captcha hostname", slog.String("givenHostname", validatedData.Hostname()))

		return PostVotingV1PollsIdVotes400JSONResponse{
			Code:    CaptchaInvalid,
			Message: "Invalid hostname, must come from icaa.world",
		}, nil
	}

	poll, err := a.db.GetPoll(ctx, request.Id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to fetch a poll", "error", err)

		if errors.As(err, &pollsErr) && pollsErr.Reason == polls.REASON_POLL_DOES_NOT_EXIST {
			return PostVotingV1PollsIdVotes404JSONResponse{
				Code:    NotFound,
				Message: "Poll does not exist",
			}, nil
		}

		return PostVotingV1PollsIdVotes500JSONResponse{
			Code:    InternalError,
			Message: "Failed to vote",
		}, nil
	}

	err = poll.ValidateBallot(openapiUUIDsToUUIDs(optionIDs), time.Now())
	if err != nil {
		span.RecordError(err)
		logger.Warn("Invalid ballot", slog.String("error", err.Error()))

		if errors.As(err, &pollsErr) {
			switch pollsErr.Reason {
			case polls.REASON_POLL_NOT_ACTIVE:
				return PostVotingV1PollsIdVotes403JSONResponse{
					Code:    PollNotActive,
					Message: "Poll is not currently active",
				}, nil
			case polls.REASON_INVALID_BALLOT:
				return PostVotingV1PollsIdVotes400JSONResponse{
					Code:    InvalidBallot,
					Message: pollsErr.Message,
				}, nil
			}
		}

		return PostVotingV1PollsIdVotes500JSONResponse{
			Code:    InternalError,
			Message: "Failed to vote",
		}, nil
	}

	now := time.Now()
	record = polls.VoteRecord{
		PollID:         request.Id,
		IdempotencyKey: request.Params.IdempotencyKey.String(),
		OptionIDs:      openapiUUIDsToUUIDs(optionIDs),
		CreatedAt:      now,
		TTL:            now.Add(voteRecordTTL),
	}

	err = a.db.RecordVote(ctx, record)
	if err != nil {
		// Lost a race with a concurrent request using the same key, treat as a replay
		if errors.As(err, &pollsErr) && pollsErr.Reason == polls.REASON_VOTE_ALREADY_RECORDED {
			existingRecord, fetchErr := a.db.GetVoteRecord(ctx, request.Id, request.Params.IdempotencyKey.String())
			if fetchErr != nil {
				span.RecordError(fetchErr)
				span.SetStatus(codes.Error, fetchErr.Error())
				logger.Error("Failed to fetch vote record after race", "error", fetchErr)

				return PostVotingV1PollsIdVotes500JSONResponse{
					Code:    InternalError,
					Message: "Failed to vote",
				}, nil
			}
			return voteRecordReplayResponse(existingRecord, optionIDs)
		}

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to record vote", "error", err)

		return PostVotingV1PollsIdVotes500JSONResponse{
			Code:    InternalError,
			Message: "Failed to vote",
		}, nil
	}

	logger.Info("recorded vote", slog.String("poll-id", request.Id.String()))

	return PostVotingV1PollsIdVotes200JSONResponse{
		PollId:    request.Id,
		OptionIds: optionIDs,
	}, nil
}

// voteRecordReplayResponse builds the response for a request whose idempotency
// key was already recorded, either as a replay of the same ballot or a
// conflict if the ballot differs
func voteRecordReplayResponse(record polls.VoteRecord, requestedOptionIDs []openapi_types.UUID) (PostVotingV1PollsIdVotesResponseObject, error) {
	if sameOptionIDs(record.OptionIDs, openapiUUIDsToUUIDs(requestedOptionIDs)) {
		return PostVotingV1PollsIdVotes200JSONResponse{
			PollId:    record.PollID,
			OptionIds: uuidsToOpenapiUUIDs(record.OptionIDs),
		}, nil
	}

	return PostVotingV1PollsIdVotes409JSONResponse{
		Code:    IdempotencyConflict,
		Message: "Idempotency key was already used with a different ballot",
	}, nil
}

func sameOptionIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}

	compareUUID := func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	}
	sortedA := slices.Clone(a)
	slices.SortFunc(sortedA, compareUUID)
	sortedB := slices.Clone(b)
	slices.SortFunc(sortedB, compareUUID)

	return slices.Equal(sortedA, sortedB)
}

func (a *API) GetVotingV1PollsIdResults(ctx context.Context, request GetVotingV1PollsIdResultsRequestObject) (GetVotingV1PollsIdResultsResponseObject, error) {
	ctx, span := a.tracer.Start(ctx, "GetVotingV1PollsIdResults")
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
		if errors.As(err, &pollsErr) && pollsErr.Reason == polls.REASON_POLL_DOES_NOT_EXIST {
			return GetVotingV1PollsIdResults404JSONResponse{
				Code:    NotFound,
				Message: "Poll does not exist",
			}, nil
		}

		return GetVotingV1PollsIdResults500JSONResponse{
			Code:    InternalError,
			Message: "Failed to get results",
		}, nil
	}

	if !poll.CanViewResults(a.isAdmin(ctx), time.Now()) {
		return GetVotingV1PollsIdResults403JSONResponse{
			Code:    AuthError,
			Message: "Results are not available for this poll",
		}, nil
	}

	results, err := a.db.GetResults(ctx, request.Id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("Failed to fetch results", "error", err)

		return GetVotingV1PollsIdResults500JSONResponse{
			Code:    InternalError,
			Message: "Failed to get results",
		}, nil
	}

	// Only report counts for options that currently exist in the poll,
	// stale counts of deleted options are filtered out
	optionResults := []OptionResult{}
	for _, o := range poll.Options {
		optionResults = append(optionResults, OptionResult{
			OptionId: o.ID,
			Count:    results.Counts[o.ID],
		})
	}

	return GetVotingV1PollsIdResults200JSONResponse{
		PollId:     results.PollID,
		TotalVotes: results.TotalVotes,
		Results:    optionResults,
	}, nil
}

func openapiUUIDsToUUIDs(ids []openapi_types.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = uuid.UUID(id)
	}
	return out
}

func uuidsToOpenapiUUIDs(ids []uuid.UUID) []openapi_types.UUID {
	out := make([]openapi_types.UUID, len(ids))
	for i, id := range ids {
		out[i] = openapi_types.UUID(id)
	}
	return out
}
