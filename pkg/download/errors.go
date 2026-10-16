package download

import (
	"context"
	"errors"
)

// Error codes returned to API clients. They are stable identifiers; messages are for people.
const (
	CodeTooLarge    = "too_large"
	CodeTimeout     = "timeout"
	CodeCanceled    = "canceled"
	CodeNetwork     = "network_error"
	CodeTLS         = "tls_error"
	CodeAuth        = "auth_required"
	CodeServer      = "server_error"
	CodeEmpty       = "empty_site"
	CodeWget        = "wget_failed"
	CodeArchive     = "archive_failed"
	CodeUpload      = "upload_failed"
	CodeUnavailable = "state_unavailable"
	CodeInternal    = "internal_error"
)

// Error is a classified job failure: a stable Code, a readable Message, and the cause.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func Fail(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Err: cause}
}

// Classify converts any error into an *Error, mapping context errors to timeout/canceled.
func Classify(err error) *Error {
	var e *Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e):
		return e
	case errors.Is(err, context.DeadlineExceeded):
		return Fail(CodeTimeout, "The job ran out of time", err)
	case errors.Is(err, context.Canceled):
		return Fail(CodeCanceled, "The job was canceled", err)
	}
	return Fail(CodeInternal, "Unexpected processing error", err)
}

func contextError(ctx context.Context) *Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Fail(CodeTimeout, "The website took too long to download", ctx.Err())
	}
	return Fail(CodeCanceled, "The job was canceled", ctx.Err())
}
