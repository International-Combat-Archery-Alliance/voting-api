package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/International-Combat-Archery-Alliance/auth"
	"github.com/International-Combat-Archery-Alliance/auth/token"
	"github.com/International-Combat-Archery-Alliance/captcha"
	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/google/uuid"
)

var noopLogger = slog.New(slog.DiscardHandler)

// newTestTokenService creates a token service for testing with a test signing key
func newTestTokenService() *token.TokenService {
	testKey := token.SigningKey{
		ID:  "test",
		Key: []byte("test-signing-key-minimum-32-characters-long"),
	}
	return token.NewTokenService(testKey)
}

// mockAuthToken implements auth.AuthToken for testing
type mockAuthToken struct {
	email   string
	isAdmin bool
}

func (m *mockAuthToken) ExpiresAt() time.Time {
	return time.Now().Add(time.Hour)
}

func (m *mockAuthToken) ProfilePicURL() string {
	return "https://example.com/profile.jpg"
}

func (m *mockAuthToken) IsAdmin() bool {
	return m.isAdmin
}

func (m *mockAuthToken) UserEmail() string {
	return m.email
}

func (m *mockAuthToken) Roles() []auth.Role {
	if m.isAdmin {
		return []auth.Role{auth.RoleAdmin}
	}
	return nil
}

type mockCaptchaValidator struct {
	ValidateFunc func(ctx context.Context, token string, remoteIP string) (captcha.ValidatedData, error)
}

type mockCaptchaValidatedData struct{}

func (m *mockCaptchaValidatedData) Hostname() string       { return "icaa.world" }
func (m *mockCaptchaValidatedData) Action() string         { return "" }
func (m *mockCaptchaValidatedData) ChallengeTS() time.Time { return time.Now() }

func (m *mockCaptchaValidator) Validate(ctx context.Context, token string, remoteIP string) (captcha.ValidatedData, error) {
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx, token, remoteIP)
	}
	return &mockCaptchaValidatedData{}, nil
}

var _ DB = &mockDB{}

type mockDB struct {
	CreatePollFunc    func(ctx context.Context, poll polls.Poll) error
	GetPollFunc       func(ctx context.Context, id uuid.UUID) (polls.Poll, error)
	GetPollsFunc      func(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error)
	UpdatePollFunc    func(ctx context.Context, poll polls.Poll) error
	DeletePollFunc    func(ctx context.Context, id uuid.UUID) error
	GetResultsFunc    func(ctx context.Context, pollID uuid.UUID) (polls.Results, error)
	GetVoteRecordFunc func(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error)
	RecordVoteFunc    func(ctx context.Context, record polls.VoteRecord) error
}

func (m *mockDB) CreatePoll(ctx context.Context, poll polls.Poll) error {
	return m.CreatePollFunc(ctx, poll)
}

func (m *mockDB) GetPoll(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
	return m.GetPollFunc(ctx, id)
}

func (m *mockDB) GetPolls(ctx context.Context, limit int32, cursor *string) (polls.GetPollsResponse, error) {
	return m.GetPollsFunc(ctx, limit, cursor)
}

func (m *mockDB) UpdatePoll(ctx context.Context, poll polls.Poll) error {
	return m.UpdatePollFunc(ctx, poll)
}

func (m *mockDB) DeletePoll(ctx context.Context, id uuid.UUID) error {
	return m.DeletePollFunc(ctx, id)
}

func (m *mockDB) GetResults(ctx context.Context, pollID uuid.UUID) (polls.Results, error) {
	return m.GetResultsFunc(ctx, pollID)
}

func (m *mockDB) GetVoteRecord(ctx context.Context, pollID uuid.UUID, idempotencyKey string) (polls.VoteRecord, error) {
	return m.GetVoteRecordFunc(ctx, pollID, idempotencyKey)
}

func (m *mockDB) RecordVote(ctx context.Context, record polls.VoteRecord) error {
	return m.RecordVoteFunc(ctx, record)
}
