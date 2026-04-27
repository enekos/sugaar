package sugaar

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError carries a status code alongside a message. Returning one from a
// handler lets the default ErrorHandler emit the right status without each
// handler having to wire it up.
//
//	if user == nil {
//	    return sugaar.NotFound("user not found")
//	}
type HTTPError struct {
	Status  int    `json:"-"`
	Code    string `json:"code,omitempty"`
	Message string `json:"error"`
	cause   error
}

func (e *HTTPError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%d %s: %v", e.Status, e.Message, e.cause)
	}
	return fmt.Sprintf("%d %s", e.Status, e.Message)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *HTTPError) Unwrap() error { return e.cause }

// WithCause attaches an underlying error without changing the public message.
func (e *HTTPError) WithCause(err error) *HTTPError {
	e.cause = err
	return e
}

// WithCode tags the error with a machine-readable code (e.g. "user_not_found").
func (e *HTTPError) WithCode(code string) *HTTPError {
	e.Code = code
	return e
}

// Common HTTP error constructors. Pass an empty string for the default phrase.
func httpErr(status int, msg string) *HTTPError {
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &HTTPError{Status: status, Message: msg}
}

func BadRequest(msg string) *HTTPError   { return httpErr(http.StatusBadRequest, msg) }
func Unauthorized(msg string) *HTTPError { return httpErr(http.StatusUnauthorized, msg) }
func Forbidden(msg string) *HTTPError    { return httpErr(http.StatusForbidden, msg) }
func NotFound(msg string) *HTTPError     { return httpErr(http.StatusNotFound, msg) }
func Conflict(msg string) *HTTPError     { return httpErr(http.StatusConflict, msg) }
func TooManyRequests(msg string) *HTTPError {
	return httpErr(http.StatusTooManyRequests, msg)
}
func Internal(msg string) *HTTPError {
	return httpErr(http.StatusInternalServerError, msg)
}

// asHTTPError extracts an *HTTPError from err's chain.
func asHTTPError(err error) (*HTTPError, bool) {
	var he *HTTPError
	if errors.As(err, &he) {
		return he, true
	}
	return nil, false
}
