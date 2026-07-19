package polls

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("github.com/International-Combat-Archery-Alliance/voting-api/polls")

type ResultsVisibility string

const (
	// RESULTS_VISIBILITY_LIVE allows anyone to view results at any time
	RESULTS_VISIBILITY_LIVE ResultsVisibility = "Live"
	// RESULTS_VISIBILITY_AFTER_CLOSE allows anyone to view results after the poll closes, admins at any time
	RESULTS_VISIBILITY_AFTER_CLOSE ResultsVisibility = "AfterClose"
	// RESULTS_VISIBILITY_ADMIN_ONLY only allows admins to view results
	RESULTS_VISIBILITY_ADMIN_ONLY ResultsVisibility = "AdminOnly"
)

type Status string

const (
	STATUS_UPCOMING Status = "Upcoming"
	STATUS_ACTIVE   Status = "Active"
	STATUS_CLOSED   Status = "Closed"
)

type Poll struct {
	ID                uuid.UUID
	Version           int
	Name              string
	Description       *string
	StartTime         time.Time
	EndTime           time.Time
	ResultsVisibility ResultsVisibility
	VoteConfig        VoteConfig
	Groups            []Group
	Options           []Option
}

type VoteConfig struct {
	// MaxSelections is the max amount of options that can be selected in a single ballot
	MaxSelections int
	// MaxSelectionsPerGroup is the max amount of options per group in a single ballot, nil for no limit
	MaxSelectionsPerGroup *int
}

type Group struct {
	ID       uuid.UUID
	Name     string
	Color    *string
	ImageURL *string
}

type Option struct {
	ID       uuid.UUID
	GroupID  *uuid.UUID
	Name     string
	Subtitle *string
	ImageURL *string
}

type GetPollsResponse struct {
	Data        []Poll
	Cursor      *string
	HasNextPage bool
}

// Status returns the computed status of the poll at the given time
func (p Poll) Status(now time.Time) Status {
	if now.Before(p.StartTime) {
		return STATUS_UPCOMING
	}
	if now.After(p.EndTime) {
		return STATUS_CLOSED
	}
	return STATUS_ACTIVE
}

// IsActive returns whether the poll is accepting votes at the given time
func (p Poll) IsActive(now time.Time) bool {
	return p.Status(now) == STATUS_ACTIVE
}

// CanViewResults returns whether the results of the poll can be viewed at the
// given time based on the poll's results visibility setting
func (p Poll) CanViewResults(isAdmin bool, now time.Time) bool {
	switch p.ResultsVisibility {
	case RESULTS_VISIBILITY_LIVE:
		return true
	case RESULTS_VISIBILITY_AFTER_CLOSE:
		return isAdmin || now.After(p.EndTime)
	case RESULTS_VISIBILITY_ADMIN_ONLY:
		return isAdmin
	default:
		return false
	}
}

// ValidatePoll validates the cross field rules of a poll that the OpenAPI spec can't cover
func ValidatePoll(poll Poll) error {
	if !poll.EndTime.After(poll.StartTime) {
		return NewInvalidPollError("end time must be after start time")
	}

	switch poll.ResultsVisibility {
	case RESULTS_VISIBILITY_LIVE, RESULTS_VISIBILITY_AFTER_CLOSE, RESULTS_VISIBILITY_ADMIN_ONLY:
	default:
		return NewInvalidPollError("unknown results visibility")
	}

	if len(poll.Options) == 0 {
		return NewInvalidPollError("poll must have at least one option")
	}

	if poll.VoteConfig.MaxSelections < 1 {
		return NewInvalidPollError("max selections must be at least 1")
	}

	if poll.VoteConfig.MaxSelections > len(poll.Options) {
		return NewInvalidPollError("max selections cannot be greater than the amount of options")
	}

	if poll.VoteConfig.MaxSelectionsPerGroup != nil {
		if *poll.VoteConfig.MaxSelectionsPerGroup < 1 {
			return NewInvalidPollError("max selections per group must be at least 1")
		}
		if *poll.VoteConfig.MaxSelectionsPerGroup > poll.VoteConfig.MaxSelections {
			return NewInvalidPollError("max selections per group cannot be greater than max selections")
		}
	}

	groupIDs := make(map[uuid.UUID]struct{}, len(poll.Groups))
	for _, g := range poll.Groups {
		if _, ok := groupIDs[g.ID]; ok {
			return NewInvalidPollError("duplicate group ID")
		}
		groupIDs[g.ID] = struct{}{}
	}

	optionIDs := make(map[uuid.UUID]struct{}, len(poll.Options))
	for _, o := range poll.Options {
		if _, ok := optionIDs[o.ID]; ok {
			return NewInvalidPollError("duplicate option ID")
		}
		optionIDs[o.ID] = struct{}{}

		if o.GroupID != nil {
			if _, ok := groupIDs[*o.GroupID]; !ok {
				return NewInvalidPollError("option references a group that does not exist")
			}
		}
	}

	return nil
}

func UpdatePoll(ctx context.Context, repo Repository, id uuid.UUID, poll Poll) (Poll, error) {
	ctx, span := tracer.Start(ctx, "UpdatePoll")
	defer span.End()

	span.SetAttributes(attribute.String("poll_id", id.String()))

	existingPoll, err := repo.GetPoll(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Poll{}, err
	}

	updatedPoll := poll
	updatedPoll.ID = id
	updatedPoll.Version = existingPoll.Version + 1

	if err := ValidatePoll(updatedPoll); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Poll{}, err
	}

	err = repo.UpdatePoll(ctx, updatedPoll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Poll{}, err
	}

	return updatedPoll, nil
}
