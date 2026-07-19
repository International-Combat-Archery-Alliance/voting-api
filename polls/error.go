package polls

import "fmt"

type ErrorReason string

const (
	REASON_FAILED_TO_TRANSLATE_TO_DB_MODEL ErrorReason = "FAILED_TO_TRANSLATE_TO_DB_MODEL"
	REASON_FAILED_TO_WRITE                 ErrorReason = "FAILED_TO_WRITE"
	REASON_POLL_DOES_NOT_EXIST             ErrorReason = "POLL_DOES_NOT_EXIST"
	REASON_POLL_ALREADY_EXISTS             ErrorReason = "POLL_ALREADY_EXISTS"
	REASON_FAILED_TO_FETCH                 ErrorReason = "FAILED_TO_FETCH"
	REASON_INVALID_CURSOR                  ErrorReason = "INVALID_CURSOR"
	REASON_TIMEOUT                         ErrorReason = "TIMEOUT"
	REASON_INVALID_POLL                    ErrorReason = "INVALID_POLL"
	REASON_POLL_NOT_ACTIVE                 ErrorReason = "POLL_NOT_ACTIVE"
	REASON_INVALID_BALLOT                  ErrorReason = "INVALID_BALLOT"
	REASON_VERSION_CONFLICT                ErrorReason = "VERSION_CONFLICT"
	REASON_VOTE_RECORD_NOT_FOUND           ErrorReason = "VOTE_RECORD_NOT_FOUND"
	REASON_VOTE_ALREADY_RECORDED           ErrorReason = "VOTE_ALREADY_RECORDED"
)

type Error struct {
	Reason  ErrorReason
	Message string
	Cause   error
}

func (e *Error) Error() string {
	s := fmt.Sprintf("%s: %s.", e.Reason, e.Message)
	if e.Cause != nil {
		s += fmt.Sprintf(" Cause: %s", e.Cause)
	}
	return s
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func newPollError(reason ErrorReason, message string, cause error) *Error {
	return &Error{
		Reason:  reason,
		Message: message,
		Cause:   cause,
	}
}

func NewFailedToWriteError(message string, cause error) *Error {
	return newPollError(REASON_FAILED_TO_WRITE, message, cause)
}

func NewFailedToTranslateToDBModelError(message string, cause error) *Error {
	return newPollError(REASON_FAILED_TO_TRANSLATE_TO_DB_MODEL, message, cause)
}

func NewPollAlreadyExistsError(message string, cause error) *Error {
	return newPollError(REASON_POLL_ALREADY_EXISTS, message, cause)
}

func NewPollDoesNotExistError(message string, cause error) *Error {
	return newPollError(REASON_POLL_DOES_NOT_EXIST, message, cause)
}

func NewFailedToFetchError(message string, cause error) *Error {
	return newPollError(REASON_FAILED_TO_FETCH, message, cause)
}

func NewInvalidCursorError(message string, cause error) *Error {
	return newPollError(REASON_INVALID_CURSOR, message, cause)
}

func NewTimeoutError(message string) *Error {
	return newPollError(REASON_TIMEOUT, message, nil)
}

func NewInvalidPollError(message string) *Error {
	return newPollError(REASON_INVALID_POLL, message, nil)
}

func NewPollNotActiveError(message string) *Error {
	return newPollError(REASON_POLL_NOT_ACTIVE, message, nil)
}

func NewInvalidBallotError(message string) *Error {
	return newPollError(REASON_INVALID_BALLOT, message, nil)
}

func NewVersionConflictError(message string, cause error) *Error {
	return newPollError(REASON_VERSION_CONFLICT, message, cause)
}

func NewVoteRecordNotFoundError(message string) *Error {
	return newPollError(REASON_VOTE_RECORD_NOT_FOUND, message, nil)
}

func NewVoteAlreadyRecordedError(message string, cause error) *Error {
	return newPollError(REASON_VOTE_ALREADY_RECORDED, message, cause)
}
